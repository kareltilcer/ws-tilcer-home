// Command home is the entrypoint for the `home` household-management service:
// it loads configuration, opens the embedded SQLite database, runs migrations,
// seeds the default board, and serves the JSON API, the websocket, and the
// built SPA on a single origin.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embed the IANA timezone database into the binary so time.LoadLocation
	// ("Europe/Prague") works in a minimal container image that ships no system
	// tzdata. Costs ~450 KB and removes a runtime dependency (PRD §10 date
	// correctness).
	_ "time/tzdata"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/dashboard"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/logging"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/config"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/ws"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	// 1. Configuration — fail fast and loud.
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	logger.Info("config loaded", "config", cfg.Redacted())
	if cfg.DevAuthBypass {
		logger.Warn("AUTH BYPASS ACTIVE — ALL REQUESTS ARE FAKE-AUTHENTICATED — DO NOT DEPLOY",
			"dev_actor", cfg.DevActorID, "dev_roles", cfg.DevActorRoles)
	}

	// 2. Database: open → migrate → verify FTS5 → seed (only when empty).
	sqldb, err := appdb.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = sqldb.Close() }()

	migFS, err := bootstrap.MigrationFS()
	if err != nil {
		return err
	}
	if err := appdb.Migrate(sqldb, migFS); err != nil {
		return err
	}
	if err := appdb.ProbeFTS5(context.Background(), sqldb); err != nil {
		return err
	}
	seeded, err := appdb.SeedIfEmpty(context.Background(), sqldb)
	if err != nil {
		return err
	}
	logger.Info("database ready", "path", cfg.DBPath, "seeded_default_board", seeded)

	// The audit spine writer — every module (and login/logout) records through it.
	sink := audit.NewSink()

	// Optional audit retention (FR-L7). Default 0 = keep forever (no-op). There is
	// no scheduler (D9); prune runs once on boot when configured.
	if cfg.LogRetentionDays > 0 {
		pruned, err := logging.Prune(context.Background(), sqldb, sink, cfg.LogRetentionDays)
		if err != nil {
			return err
		}
		logger.Info("audit prune", "retention_days", cfg.LogRetentionDays, "pruned", pruned)
	}

	// 3. Mode B auth + session (D23, D29). Home hosts login and owns its session;
	// the browser carries no token. Under the dev bypass a fixed actor is injected
	// and no auth service / session store is used, so the app runs offline.
	authConf := auth.Config{
		RoleRefresh: time.Duration(cfg.RoleRefreshMinutes) * time.Minute,
		SessionTTL:  time.Duration(cfg.SessionTTLDays) * 24 * time.Hour,
		Secure:      cfg.IsProduction(), // TLS-only cookies in production (PRD §8)
		Origins:     cfg.AllowedOrigins,
		Logger:      logger,
	}
	var sessions *auth.SessionStore
	if cfg.DevAuthBypass {
		authConf.BypassActor = &reqctx.Actor{
			UserID: cfg.DevActorID,
			Type:   "user",
			Label:  cfg.DevActorID,
			Roles:  cfg.DevActorRoles,
		}
	} else {
		sessions = auth.NewSessionStore(sqldb)
		authConf.Sessions = sessions
		authConf.Authr = auth.NewHTTPAuthenticator(cfg.AuthBaseURL, cfg.AuthServiceSecret, cfg.AuthJWTSecret, cfg.AuthJWTIssuer, cfg.SiteKey, logger)
	}
	authHandler := auth.NewHandler(authConf, sqldb, sink)
	sessionMW := auth.NewSessionAuth(authConf)
	csrfMW := auth.NewCSRF(cfg.AllowedOrigins, cfg.DevAuthBypass)

	// 4. Websocket hub — session-authenticated on connect (the browser sends the
	// session cookie on a same-origin upgrade; no bearer token). Feature modules
	// publish change events so open boards and dashboards stay live.
	hub := ws.NewHub()
	wsCfg := ws.Config{BypassActor: authConf.BypassActor, Logger: logger}
	if sessions != nil {
		wsCfg.Authenticate = func(r *http.Request) (reqctx.Actor, bool) {
			c, err := r.Cookie("session")
			if err != nil || c.Value == "" {
				return reqctx.Actor{}, false
			}
			s, ok, err := sessions.Lookup(r.Context(), c.Value, time.Now())
			if err != nil || !ok {
				return reqctx.Actor{}, false
			}
			return reqctx.Actor{UserID: s.UserID, Type: "user", Label: s.Email, Roles: s.Roles}, true
		}
	}
	wsHandler := hub.Handler(wsCfg)

	// 5. Feature modules, composed through the registry (PRD §10 D25). Each module
	// owns its routes/migrations/audit actions/widgets; the core only wires them.
	// Modules publish websocket change events via the hub after commit.
	notify := func(typ string, payload any) { hub.Publish(ws.Message{Type: typ, Payload: payload}) }

	todoSvc := todo.NewService(sqldb, sink, notify)
	eventsSvc := events.NewService(sqldb, sink, notify, cfg.RRuleMaxOccurrences, cfg.RRuleMaxWindowMonths)

	loggingMod := logging.New(sqldb)
	todoMod := todo.NewModule(todoSvc)
	eventsMod := events.NewModule(eventsSvc, cfg.Timezone, cfg.DashboardLookbackDays)

	// The dashboard host renders widgets contributed by the feature modules — it
	// reaches feature data only through this catalog, never their tables (D28).
	catalog, err := registry.NewCatalog(registry.CollectWidgets([]registry.Module{todoMod, eventsMod}))
	if err != nil {
		return err
	}
	dashMod := dashboard.NewModule(catalog, sqldb)

	modules := []registry.Module{loggingMod, todoMod, eventsMod, dashMod}
	mountAPI := func(api chi.Router) { registry.MountAll(api, modules) }

	// 6. HTTP server.
	handler := httpx.NewRouter(httpx.Deps{
		Logger:       logger,
		DB:           sqldb,
		Site:         cfg.SiteKey,
		InsecureAuth: cfg.DevAuthBypass,
		MountAuth:    func(api chi.Router) { authHandler.Mount(api, csrfMW) },
		SessionMW:    sessionMW,
		CSRFMW:       csrfMW,
		MountAPI:     mountAPI,
		WS:           wsHandler,
		StaticDir:    cfg.StaticDir,
	})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 7. Serve until interrupted, then shut down gracefully.
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serveErr:
		return err
	case <-stop:
		logger.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}
