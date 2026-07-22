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
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/config"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/dashboard"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/ws"
)

// tokenTTLCap bounds how long an introspection result is cached (tokens live
// ~15 minutes; PRD D2).
const tokenTTLCap = 15 * time.Minute

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

	if err := appdb.Migrate(sqldb); err != nil {
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

	// Optional audit retention (FR-L7). Default 0 = keep forever (no-op). There is
	// no scheduler (D9); prune runs once on boot when configured.
	if cfg.LogRetentionDays > 0 {
		pruned, err := audit.Prune(context.Background(), sqldb, audit.NewSink(), cfg.LogRetentionDays)
		if err != nil {
			return err
		}
		logger.Info("audit prune", "retention_days", cfg.LogRetentionDays, "pruned", pruned)
	}

	// 3. Auth: real introspection (cached) in normal operation; a fake actor when
	// the dev bypass is active (development only — config refuses it in prod).
	var authCfg httpx.AuthConfig
	if cfg.DevAuthBypass {
		authCfg.BypassActor = &reqctx.Actor{
			UserID: cfg.DevActorID,
			Type:   "user",
			Label:  cfg.DevActorID,
			Roles:  cfg.DevActorRoles,
		}
	} else {
		introspector := auth.NewHTTPIntrospector(cfg.AuthBaseURL, cfg.AuthServiceSecret, cfg.SiteKey)
		authCfg.Introspector = auth.NewCachingIntrospector(introspector, tokenTTLCap)
	}

	// 4. Websocket hub (feature modules publish change events to it in later
	// phases so open boards and dashboards stay live).
	hub := ws.NewHub()
	wsHandler := hub.Handler(ws.Config{
		Introspector: authCfg.Introspector,
		BypassActor:  authCfg.BypassActor,
		Logger:       logger,
	})

	// 5. Feature API surface. Modules publish websocket change events via the hub
	// after commit; the audit log browser is admin-only.
	notify := func(typ string, payload any) { hub.Publish(ws.Message{Type: typ, Payload: payload}) }
	sink := audit.NewSink()

	logs := audit.NewHTTPHandler(audit.NewStore(sqldb))
	todoSvc := todo.NewService(sqldb, sink, notify)
	eventsSvc := events.NewService(sqldb, sink, notify, cfg.RRuleMaxOccurrences, cfg.RRuleMaxWindowMonths)
	dashSvc := dashboard.NewService(todoSvc.Store(), eventsSvc.Store(), cfg.Timezone, cfg.DashboardLookbackDays, cfg.RRuleMaxOccurrences)

	todoHandler := todo.NewHandler(todoSvc)
	eventsHandler := events.NewHandler(eventsSvc)
	dashHandler := dashboard.NewHandler(dashSvc)
	mountAPI := func(api chi.Router) {
		todoHandler.Mount(api)
		eventsHandler.Mount(api)
		dashHandler.Mount(api)
		api.Route("/logs", func(r chi.Router) {
			r.Use(httpx.RequireAdmin)
			logs.Mount(r)
		})
	}

	// 6. HTTP server.
	handler := httpx.NewRouter(httpx.Deps{
		Logger:       logger,
		DB:           sqldb,
		Site:         cfg.SiteKey,
		InsecureAuth: cfg.DevAuthBypass,
		Auth:         authCfg,
		WS:           wsHandler,
		MountAPI:     mountAPI,
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
