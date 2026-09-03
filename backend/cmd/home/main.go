// Command home is the entrypoint for the `home` household-management service:
// it loads configuration, opens the embedded SQLite database, runs migrations,
// seeds the default board, and serves the JSON API, the websocket, and the
// built SPA on a single origin.
package main

import (
	"cmp"
	"context"
	"database/sql"
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
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/admin"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/dashboard"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/documents"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/electricity"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/finance"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/garden"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/logging"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/config"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/scheduler"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/statusreport"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/ws"
)

func main() {
	stdout := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})

	// The crash reporter is built BEFORE anything else, from the environment
	// directly, because the two crashes it most needs to see both happen before
	// there is a config: a database that will not migrate and a bad variable. It
	// re-reads the same three names config validates below — the duplication buys
	// coverage of the boot itself, and config remains the only thing that can
	// refuse a half-configured one.
	reporter := statusreport.New(
		os.Getenv(config.EnvStatusIngestURL),
		os.Getenv(config.EnvStatusIngestKey),
		// The same fallback config applies, spelled with cmp.Or because there is no
		// validated Config to read it off yet.
		statusreport.WithEnvironment(cmp.Or(os.Getenv(config.EnvStatusEnvironment), os.Getenv("HOME_ENV"), "development")),
		statusreport.WithRelease(os.Getenv(config.EnvStatusRelease)),
	)
	// Every logger.Error in the process now also lands on the crash board; the
	// wrapper is a no-op when reporting is off. slog.SetDefault matters as much as
	// the argument to run: nine constructors fall back to slog.Default() when they
	// are given no logger, and a module holding the unwrapped one would log its
	// faults to Coolify alone.
	logger := slog.New(statusreport.NewLogHandler(stdout, reporter))
	slog.SetDefault(logger)

	// A panic that unwinds this goroutine ends the process, so it is reported
	// synchronously and re-raised. It is one of exactly two `fatal` events home
	// sends; every panic something recovers from is an `error`.
	defer reporter.Recover()

	if err := run(logger); err != nil {
		// The other one. It is captured explicitly rather than through the log
		// forwarder for two reasons: the level is fatal (the process is about to
		// exit, and this is the crash-loop a failed migration produces), and the
		// send must finish before os.Exit — which skips both defers and in-flight
		// goroutines. The line itself is written through the UNWRAPPED handler so
		// the same failure is not also filed as an `error`.
		reporter.CaptureSync(statusreport.Report{
			Message: "home failed to start: " + err.Error(),
			Level:   statusreport.LevelFatal,
		})
		slog.New(stdout).Error("fatal", "err", err)
		os.Exit(1)
	}
}

// run composes the process and blocks until it is asked to stop.
//
// The numbered steps are the steps of the boot: each is one function below, in
// the order it runs, and the value one step returns is what the next steps take.
// Nothing here decides anything — the decisions are in the functions, with the
// comments that explain them — so what this reads as is the wiring ORDER, which
// is the part of composition that is genuinely load-bearing. Three edges in
// particular are order and not preference: the hub exists before auth, chat is
// built after documents, and the shutdown sequence at the end joins the tailer
// before it flushes.
func run(logger *slog.Logger) error {
	// 1. Configuration — fail fast and loud.
	cfg, err := loadConfig(logger)
	if err != nil {
		return err
	}

	// 2. Database: open → migrate → verify FTS5 → seed (only when empty).
	sqldb, err := openDatabase(cfg, logger)
	if err != nil {
		return err
	}
	defer func() { _ = sqldb.Close() }()

	// The audit spine writer — every module (and login/logout) records through it.
	sink := audit.NewSink()
	if err := pruneAudit(cfg, sqldb, sink, logger); err != nil {
		return err
	}

	// 3. Mode B auth + session (D23, D29), the shared push channel, and the
	// websocket hub.
	//
	// The hub is built here rather than at step 4 because AUTH needs it: revoking
	// a session has to close that member's sockets too, or the socket keeps
	// delivering private payloads to an account that is 401 everywhere else (v10).
	hub := ws.NewHub(logger)
	authp := buildAuth(cfg, sqldb, sink, hub, logger)
	pushp := buildPush(cfg, sqldb, sink, logger)

	// 4. Websocket hub — session-authenticated on connect (the browser sends the
	// session cookie on a same-origin upgrade; no bearer token). Feature modules
	// publish change events so open boards and dashboards stay live.
	wsHandler := hub.Handler(wsConfig(cfg, authp))

	// 5. Feature modules, composed through the registry (PRD §10 D25).
	app, err := buildModules(cfg, sqldb, sink, hub, pushp, logger)
	if err != nil {
		return err
	}

	// Background workers live for the process's lifetime and stop with this context,
	// which is cancelled on the shutdown signal below.
	bgCtx, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	notifier := startBackground(bgCtx, cfg, sqldb, sink, app, logger)

	// 6. HTTP server.
	handler := httpx.NewRouter(httpx.Deps{
		Logger:       logger,
		DB:           sqldb,
		Site:         cfg.SiteKey,
		InsecureAuth: cfg.DevAuthBypass,
		MountAuth:    func(api chi.Router) { authp.handler.Mount(api, authp.csrfMW) },
		SessionMW:    authp.sessionMW,
		CSRFMW:       authp.csrfMW,
		MountAPI: func(api chi.Router) {
			// /api/push/** is platform, not a module: every member (reader included)
			// manages their own device here, whether or not any module sends anything.
			pushp.handler.Mount(api)
			app.mountAPI(api)
		},
		WS:        wsHandler,
		StaticDir: cfg.StaticDir,
	})

	// 7. Serve until interrupted, then shut down gracefully.
	return serve(cfg, logger, handler, shutdownDeps{
		stopBackground: stopBackground,
		preview:        app.preview,
		notifier:       notifier,
		admin:          app.admin,
	})
}

// ---------------------------------------------------------------------------
// 1. Configuration
// ---------------------------------------------------------------------------

// loadConfig reads the environment, then reports what it corrected and what it
// switched off.
func loadConfig(logger *slog.Logger) (*config.Config, error) {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return nil, err
	}
	logger.Info("config loaded", "config", cfg.Redacted())
	// Values that were corrected rather than refused — a window clamped to its
	// ceiling. Logged loudly here because the process is UP: refusing them instead
	// would have put the only explanation inside a restart loop.
	for _, w := range cfg.Warnings {
		logger.Warn("CONFIGURATION CORRECTED — " + w)
	}
	if cfg.DevAuthBypass {
		logger.Warn("AUTH BYPASS ACTIVE — ALL REQUESTS ARE FAKE-AUTHENTICATED — DO NOT DEPLOY",
			"dev_actor", cfg.DevActorID, "dev_roles", cfg.DevActorRoles)
	}
	// Crash reporting says its state ONCE, here, because it will never say
	// anything again: the client drops every ingest failure in silence, which is
	// the property that keeps it from taking the app down and also the reason a
	// wrong key looks exactly like a quiet week. This line is the confirmation.
	if cfg.Status.Enabled() {
		logger.Info("statusreport: crash reporting ready", "ingest", cfg.Status.IngestURL,
			"environment", cfg.Status.Environment, "release", cfg.Status.Release)
	} else {
		logger.Info("statusreport: DISABLED — no ingest endpoint configured; errors and panics stay in these logs " +
			"(set STATUS_INGEST_URL + STATUS_INGEST_KEY from the status.tilcer.cz dashboard)")
	}
	return cfg, nil
}

// ---------------------------------------------------------------------------
// 2. Database
// ---------------------------------------------------------------------------

// openDatabase opens the SQLite file, migrates it, verifies FTS5 and seeds the
// default board when the database is empty.
//
// Every failure below closes the handle before returning it as an error: the
// caller only gets a *sql.DB it is expected to defer a Close on, so the failure
// paths have to do it themselves.
func openDatabase(cfg *config.Config, logger *slog.Logger) (*sql.DB, error) {
	sqldb, err := appdb.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*sql.DB, error) {
		_ = sqldb.Close()
		return nil, err
	}

	// WithSeed: the server — and only the server — also applies the one-off
	// migration carrying `fin`'s historic months (v6, D91). It is INSERT OR
	// IGNORE, so it is a no-op on every boot after the first. Tests migrate the
	// schema-only bootstrap.MigrationFS().
	migFS, err := bootstrap.MigrationFSWithSeed()
	if err != nil {
		return fail(err)
	}
	if err := appdb.Migrate(sqldb, migFS); err != nil {
		return fail(err)
	}
	if err := appdb.ProbeFTS5(context.Background(), sqldb); err != nil {
		return fail(err)
	}
	seeded, err := appdb.SeedIfEmpty(context.Background(), sqldb)
	if err != nil {
		return fail(err)
	}
	logger.Info("database ready", "path", cfg.DBPath, "seeded_default_board", seeded)
	return sqldb, nil
}

// pruneAudit applies the optional audit retention (FR-L7). Default 0 = keep
// forever (no-op). There is no scheduler (D9); prune runs once on boot when
// configured.
func pruneAudit(cfg *config.Config, sqldb *sql.DB, sink audit.Sink, logger *slog.Logger) error {
	if cfg.LogRetentionDays <= 0 {
		return nil
	}
	pruned, err := logging.Prune(context.Background(), sqldb, sink, cfg.LogRetentionDays)
	if err != nil {
		return err
	}
	logger.Info("audit prune", "retention_days", cfg.LogRetentionDays, "pruned", pruned)
	return nil
}

// ---------------------------------------------------------------------------
// 3. Auth and push
// ---------------------------------------------------------------------------

// authParts is what the rest of the process needs from the auth strand.
type authParts struct {
	conf      auth.Config
	handler   *auth.Handler
	sessionMW func(http.Handler) http.Handler
	csrfMW    func(http.Handler) http.Handler
	// sessions is NIL under the dev bypass, and step 4 reads it as exactly that
	// question: with no session store there is nothing for a socket to
	// re-authenticate against, so the whole revalidation strand is left unset.
	sessions *auth.SessionStore
}

// buildAuth wires Mode B auth + session (D23, D29). Home hosts login and owns
// its session; the browser carries no token. Under the dev bypass a fixed actor
// is injected and no auth service / session store is used, so the app runs
// offline.
func buildAuth(cfg *config.Config, sqldb *sql.DB, sink audit.Sink, hub *ws.Hub, logger *slog.Logger) authParts {
	conf := auth.Config{
		RoleRefresh:      time.Duration(cfg.RoleRefreshMinutes) * time.Minute,
		SessionTTL:       time.Duration(cfg.SessionTTLDays) * 24 * time.Hour,
		Secure:           cfg.IsProduction(), // TLS-only cookies in production (PRD §8)
		Origins:          cfg.AllowedOrigins,
		Logger:           logger,
		OnSessionRevoked: hub.DisconnectSession,
	}
	var sessions *auth.SessionStore
	if cfg.DevAuthBypass {
		conf.BypassActor = &reqctx.Actor{
			UserID: cfg.DevActorID,
			Type:   "user",
			Label:  cfg.DevActorID,
			Roles:  cfg.DevActorRoles,
		}
	} else {
		sessions = auth.NewSessionStore(sqldb)
		conf.Sessions = sessions
		conf.Authr = auth.NewHTTPAuthenticator(cfg.AuthBaseURL, cfg.AuthServiceSecret, cfg.AuthJWTSecret, cfg.AuthJWTIssuer, cfg.SiteKey, logger)
	}
	return authParts{
		conf:      conf,
		handler:   auth.NewHandler(conf, sqldb, sink),
		sessionMW: auth.NewSessionAuth(conf),
		csrfMW:    auth.NewCSRF(cfg.AllowedOrigins, cfg.DevAuthBypass),
		sessions:  sessions,
	}
}

// pushParts is the shared Web Push channel and its HTTP surface.
type pushParts struct {
	svc     *push.Service
	store   *push.Store
	handler *push.Handler
}

// buildPush wires the shared Web Push channel (v5, D52). One service worker on
// one origin ⇒ one subscription per device ⇒ one channel every module sends
// through. It is platform infrastructure, not the admin module: a member's
// consent and mutes must outlive whatever module wants to notify them (D53).
//
// With no VAPID keypair the channel is inert — subscribing is refused with a
// clear reason and nothing is ever sent — so the whole app still runs.
func buildPush(cfg *config.Config, sqldb *sql.DB, sink audit.Sink, logger *slog.Logger) pushParts {
	store := push.NewStore(sqldb)
	svc := push.NewService(store, push.Config{
		VAPIDPublicKey:  cfg.Notif.VAPIDPublicKey,
		VAPIDPrivateKey: cfg.Notif.VAPIDPrivateKey,
		VAPIDSubject:    cfg.Notif.VAPIDSubject,
		MaxFailDays:     cfg.Notif.MaxFailDays,
		// A subscription's endpoint decides where this server POSTs, so it is
		// allowlisted to the known push services rather than taken on trust from
		// whoever is signed in; HOME_PUSH_ENDPOINT_HOSTS extends the built-in list.
		AllowedEndpointHosts: push.EndpointHosts(cfg.Notif.PushEndpointHosts),
		Logger:               logger,
	})
	if svc.Enabled() {
		logger.Info("push: channel ready", "subject", cfg.Notif.VAPIDSubject)
	} else {
		logger.Warn("push: DISABLED — no VAPID keypair configured; notifications will not be sent " +
			"(set HOME_VAPID_PUBLIC_KEY / HOME_VAPID_PRIVATE_KEY / HOME_VAPID_SUBJECT; generate with `go run ./cmd/vapidgen`)")
	}
	return pushParts{svc: svc, store: store, handler: push.NewHandler(svc, sqldb, sink)}
}

// ---------------------------------------------------------------------------
// 4. Websocket
// ---------------------------------------------------------------------------

// wsConfig builds the hub's configuration. With no session store (the dev
// bypass) the three session seams below are left nil and the hub falls back to
// the bypass actor.
func wsConfig(cfg *config.Config, a authParts) ws.Config {
	wsCfg := ws.Config{
		BypassActor:     a.conf.BypassActor,
		RevalidateEvery: time.Duration(cfg.WSRevalidateMinutes) * time.Minute,
	}
	if a.sessions == nil {
		return wsCfg
	}
	// The upgrade decision. Collapsing a store error onto "reject" is right
	// HERE — refusing a new connection costs a reconnect and the browser is
	// already backing off — and wrong for the revalidation below, which would
	// be tearing down live sockets instead.
	wsCfg.Authenticate = func(r *http.Request) (ws.Upgrade, bool) {
		c, err := r.Cookie("session")
		if err != nil || c.Value == "" {
			return ws.Upgrade{}, false
		}
		s, ok, err := a.sessions.Lookup(r.Context(), c.Value, time.Now())
		if err != nil || !ok {
			return ws.Upgrade{}, false
		}
		return ws.Upgrade{
			Actor:     reqctx.Actor{UserID: s.UserID, Type: "user", Label: s.Email, Roles: s.Roles},
			SessionID: s.ID,
			Token:     c.Value,
		}, true
	}
	// ⚠ The pump re-takes the decision through auth's OWN revalidation, not
	// through a second Lookup here. Re-checking only that the session row is
	// live is what every HTTP request does NOT do: the middleware fails closed
	// on a re-mint that says the account is disabled, and a socket whose tab
	// issues no HTTP request would otherwise never meet that check and keep
	// receiving targeted payloads for the whole session TTL. It also reports
	// "could not tell" apart from "revoked", so a slow query does not close
	// every socket in the household.
	wsCfg.Revalidate = func(ctx context.Context, token string) (string, ws.Revalidation) {
		userID, verdict := a.conf.RevalidateSession(ctx, token)
		return userID, wsRevalidation(verdict)
	}
	// ⚠ The CONNECT-TIME check is a bare row check (auth.CheckSession),
	// deliberately weaker than the pump's. The hole it closes is a revocation
	// that landed between the upgrade decision and the hub registration, and
	// the revoked row is enough to see that. Pointing it at RevalidateSession
	// instead meant a second Lookup and — whenever roles were stale — a Mint
	// PER SOCKET: on a deploy every tab in the household redials at once, and
	// none of those mints sees another's roles_refreshed_at stamp, so they all
	// go to the auth service together, over a pool of exactly one connection.
	// The fail-closed re-mint still runs, on the session's ticker, once per
	// session per interval.
	//
	// Both seams cross the verdict boundary through the SAME pinned pieces:
	// auth's SessionVerdict (TestCheckSession) and the wsRevalidation bridge
	// (TestWSRevalidation). An inline three-state mapping here was the one arm
	// of this boundary nothing pinned.
	wsCfg.Recheck = func(ctx context.Context, token string) (string, ws.Revalidation) {
		userID, verdict := a.conf.CheckSession(ctx, token)
		return userID, wsRevalidation(verdict)
	}
	return wsCfg
}

// ---------------------------------------------------------------------------
// 5. Feature modules and the four catalogs
// ---------------------------------------------------------------------------

// appModules is step 5's output: the mounted API, plus the handful of services
// the server and the background workers still need by name.
type appModules struct {
	mountAPI func(chi.Router)

	admin  *admin.Service
	garden *garden.Service
	chat   *chat.Service
	docs   *documents.Service
	notes  *notes.Service

	preview *documents.PreviewWorker
	// blob and backup are the documents object store and its mirror target. They
	// are here because the two mirror jobs in step 5c take them, and those are
	// started after this function returns.
	blob   blobstore.BlobStore
	backup blobstore.BlobStore
}

// buildModules constructs every feature module and the four catalogs they
// publish into (PRD §10 D25). Each module owns its routes/migrations/audit
// actions/widgets; the core only wires them. Modules publish websocket change
// events via the hub after commit. The push carries the originating request's
// client id (from reqctx) so each browser tab can tell its own echo apart from a
// change made on another device. hub.Notify stamps the Origin; hub.NotifyTo is
// its member-restricted sibling, so a targeted module gets the same
// echo-suppression without re-deriving the client id for itself (v10).
func buildModules(cfg *config.Config, sqldb *sql.DB, sink audit.Sink, hub *ws.Hub, pushp pushParts, logger *slog.Logger) (appModules, error) {
	notify := hub.Notify

	todoSvc := todo.NewService(sqldb, sink, notify)
	eventsSvc := events.NewService(sqldb, sink, notify, cfg.RRuleMaxOccurrences, cfg.RRuleMaxWindowMonths)

	// finance (v6) is the simplest module here: one table, no blob store, no
	// scheduler, no external call, no config of its own. It replaces the
	// standalone `fin` service, whose historic months arrive through the seed
	// migration source applied above.
	financeSvc := finance.NewService(sqldb, sink, notify)

	// electricity (v8) takes the timezone because that is the ONE place a clock
	// enters the module: `today` is resolved per request in the service and
	// travels into the pure compute.go on the snapshot, so all three computed
	// endpoints agree about what day it is even across a midnight.
	elecSvc := electricity.NewService(sqldb, sink, notify, cfg.Timezone)

	// garden (v7) is the largest module here — eleven tables — but it wires in
	// like any other: no blob store, no push, no new platform strand. Its ONE
	// external dependency is a public forecast, polled through the scheduler hook
	// registered below, and it is soft: every failure is logged and swallowed.
	gardenSvc := garden.NewService(sqldb, sink, notify, garden.Options{
		Location:       cfg.Timezone,
		WeatherEnabled: cfg.Garden.WeatherEnabled,
		WeatherURL:     cfg.Garden.WeatherURL,
		WeatherPoll:    time.Duration(cfg.Garden.WeatherPollHours) * time.Hour,
		ImportMaxBytes: cfg.Garden.ImportMaxBytes,
		Logger:         logger,
	})

	// documents (v4) is the first module with bytes outside SQLite: it needs an object
	// store, an async preview worker, and — because Litestream cannot back up a blob
	// bucket — its own mirror/reconciliation job (D45).
	docsBlob, docsBackup, err := openDocumentStores(cfg, logger)
	if err != nil {
		return appModules{}, err
	}
	docsSvc := documents.NewService(sqldb, sink, notify, docsBlob, documents.Options{
		MaxUploadBytes: int64(cfg.Docs.MaxUploadMB) << 20,
		AllowedMIME:    cfg.Docs.AllowedMIME,
		PreviewEnabled: cfg.Docs.PreviewEnabled,
		PublicBaseURL:  cfg.Docs.PublicBaseURL,
	}, logger)

	// chat (v10) is the ONLY module here that does not take `notify`. Every other
	// one broadcasts "something changed" to every connected client; chat's payload
	// IS the content, so it takes hub.NotifyTo instead and names its audience per
	// message (D232/D233). That method exists because PR 1 taught the hub who is
	// connected — nothing else in this file changed for it.
	//
	// It also takes pushStore twice over, in two different roles: as the member
	// DIRECTORY (projected from `sessions` — Home has no user table) and, through
	// pushSvc, as the notification channel. Chat narrows the directory to user id
	// and display name at its own boundary, because /api/chat/directory is the
	// first surface in Home that shows it to a non-admin (D230).
	//
	// It is constructed AFTER documents, and the order is the dependency:
	// `documents` is chat's `storage.BlobSink` — the receiving half of the move
	// (D238) — handed over as a plain interface value at composition, because
	// internal/arch forbids the two modules from importing each other.
	//
	// ⚠ IT IS OPTIONAL AND NIL MEANS 501, NEVER A FALLBACK TO DELETE (D239). Wiring
	// the wrong VALUE here compiles and passes every test — §V9-12 records a catalog
	// method landing on the wrong type, everything green, and the page reporting
	// 0 B — so TestDocumentsImplementsBlobSink pins the interface and this line is
	// the one place the value is chosen.
	//
	// ⚠ AND THE UPLOAD SETTINGS ARE DOKUMENTY'S, DELIBERATELY (D228). The per-file
	// cap is HOME_DOCS_MAX_UPLOAD_MB because a file above Dokumenty's cap could never
	// be MOVED into Dokumenty — the clean-up page's headline action would fail on
	// exactly the files heavy enough to have caused the overrun. The thumbnail
	// toolchain is shared for the same reason there is only one of it in the image.
	chatSvc := chat.NewService(sqldb, sink, hub.NotifyTo, pushp.svc, pushp.store, chat.Options{
		TrashDays: cfg.ChatTrashDays,
		Logger:    logger,
		Blob:      docsBlob,
		Sink:      docsSvc,
		Upload: chat.UploadOptions{
			MaxBytes: int64(cfg.Docs.MaxUploadMB) << 20,
			// TempDir stays the OS default, as it does for Dokumenty: nothing in this
			// image configures one, and a staged upload lives for the length of one
			// request.
			Thumb: chat.ThumbOptions{
				CwebpPath:      cfg.Docs.CwebpPath,
				MaxPx:          cfg.Docs.ThumbMaxPx,
				MaxImagePixels: cfg.Docs.ImageMaxMegapixels * 1_000_000,
			},
		},
	})

	// notes (v4.1) gained inline images: the bytes reuse the documents object store
	// under a distinct note-images/ prefix (each module's reconciliation is prefix-
	// scoped, so the two never touch each other's objects) and share its backup mirror.
	notesSvc := notes.NewService(sqldb, sink, notify, docsBlob, notes.ImageOptions{
		MaxUploadBytes: int64(cfg.NotesImageMaxUploadMB) << 20,
		GCGrace:        2 * time.Minute, // spare an in-flight upload from a racing body save
	}, logger)

	previewWorker := documents.NewPreviewWorker(docsSvc.Store(), docsBlob, notify, documents.PreviewConfig{
		Enabled:        cfg.Docs.PreviewEnabled,
		GotenbergURL:   cfg.Docs.GotenbergURL,
		Timeout:        cfg.Docs.PreviewTimeout,
		Workers:        cfg.Docs.PreviewWorkers,
		PdftoppmPath:   cfg.Docs.PdftoppmPath,
		CwebpPath:      cfg.Docs.CwebpPath,
		ThumbMaxPx:     cfg.Docs.ThumbMaxPx,
		MaxImagePixels: cfg.Docs.ImageMaxMegapixels * 1_000_000,
		Logger:         logger,
	})
	docsSvc.SetPreviewEnqueue(previewWorker.Enqueue)

	loggingMod := logging.New(sqldb)
	todoMod := todo.NewModule(todoSvc)
	eventsMod := events.NewModule(eventsSvc, cfg.Timezone, cfg.DashboardLookbackDays)
	notesMod := notes.NewModule(notesSvc)
	docsMod := documents.NewModule(docsSvc)
	financeMod := finance.NewModule(financeSvc, cfg.Timezone)
	gardenMod := garden.NewModule(gardenSvc)

	// electricity (v8) is the LEANEST module here. It appears in exactly ONE
	// collection below — featureModules — and is deliberately absent from
	// CollectWidgets, metrics.Collect and lists.Collect (D147): it publishes
	// nothing to Nástěnka and nothing to the notification catalogs.
	elecMod := electricity.NewModule(elecSvc)

	// chat (v10) joins featureModules and the storage catalog, and NOTHING else.
	// It is deliberately absent from CollectWidgets, metrics.Collect and
	// lists.Collect (D252) — the electricity precedent, enforced this time by
	// forbiddenImports["chat"] rather than only by not registering.
	chatMod := chat.NewModule(chatSvc)

	// THE SIX CONTRIBUTING MODULES, written once. They are the modules that
	// publish to the household surfaces: the dashboard's widget catalog, the
	// metric catalog and the list catalog, all three below.
	//
	// ⚠ IT IS A NAMED SLICE BECAUSE THE THREE LISTS HAD TO STAY IN STEP AND
	// NOTHING CHECKED THAT THEY DID. `electricity` (D147) and `chat` (D252) are
	// deliberately absent from ALL THREE — the comments on elecMod and chatMod
	// above say so at length — and "absent from all three" is exactly the
	// invariant one name expresses and three literals cannot. A module added to
	// two of them and forgotten in the third used to be a silent bug.
	//
	// Not to be confused with featureModules below, which is a DIFFERENT and
	// longer list: those ten are every module the action catalog reads verbs
	// from, electricity, chat, logging and dashboard included.
	contributingModules := []registry.Module{todoMod, eventsMod, notesMod, docsMod, financeMod, gardenMod}
	// metrics.Collect and lists.Collect take `...any` where CollectWidgets takes
	// []registry.Module, and Go will not spread the one into the other — so the
	// same six need a []any beside them rather than a second literal.
	contributingAny := make([]any, len(contributingModules))
	for i, m := range contributingModules {
		contributingAny[i] = m
	}

	// The dashboard host renders widgets contributed by the feature modules — it
	// reaches feature data only through this catalog, never their tables (D28).
	catalog, err := registry.NewCatalog(registry.CollectWidgets(contributingModules))
	if err != nil {
		return appModules{}, err
	}
	dashMod := dashboard.NewModule(catalog, sqldb)

	// 5b. v5: the metrics catalog — the THIRD registered catalog, beside widgets
	// and audit actions. Modules publish counts; the admin module's summaries
	// reference them by key and never touch a feature table (D59/D28).
	featureModules := []registry.Module{loggingMod, todoMod, eventsMod, notesMod, docsMod, financeMod, gardenMod, elecMod, chatMod, dashMod}
	metricRegistry, err := metrics.Collect(contributingAny...)
	if err != nil {
		return appModules{}, err
	}
	// The list catalog beside it: the same modules also publish WHICH items are
	// behind those counts, so a summary can name today's reminders instead of
	// only counting them. Collected the same way, read through the same kind of
	// registry, so admin still imports no feature module.
	listRegistry, err := lists.Collect(contributingAny...)
	if err != nil {
		return appModules{}, err
	}
	// The action catalog the trigger composer picks from: every module's declared
	// verbs plus the platform-emitted ones (login, push.*), which belong to no
	// module and would otherwise be unbindable.
	actions := registry.CollectActions(featureModules)
	for _, key := range audit.PlatformActions {
		actions = append(actions, registry.Action{Key: key, Module: audit.ModulePlatform})
	}

	adminSvc := admin.NewService(sqldb, sink, admin.Options{
		Sender:          pushp.svc,
		PushStore:       pushp.store,
		Metrics:         metricRegistry,
		Lists:           listRegistry,
		Actions:         actions,
		Location:        cfg.Timezone,
		DefaultCoalesce: cfg.Notif.CoalesceDefault,
		Logger:          logger,
	})
	adminMod := admin.NewModule(adminSvc)
	// The delivery log's table belongs to the admin module, so platform/push
	// records through it rather than writing another module's table directly.
	pushp.svc.SetRecorder(adminSvc.Store())

	modules := append(featureModules, adminMod)

	// 5c. v9: the storage catalog — the FOURTH registered catalog, beside widgets,
	// audit actions, and the metric/list pair. Modules declare the SQLite tables
	// they own and (for the two that hold bytes) their attributed R2 usage; the
	// Úložiště page reads the registry and `admin` still imports no feature module
	// (D191). It is collected from the FULL module set, admin included, because
	// admin owns tables too.
	storageCatalog, err := storage.Collect(toAny(modules)...)
	if err != nil {
		return appModules{}, err
	}
	adminSvc.SetStorage(admin.NewStorageService(admin.StorageDeps{
		DB:            sqldb,
		DBPath:        cfg.DBPath,
		Catalog:       storageCatalog,
		Primary:       docsBlob,
		PrimaryBucket: cfg.Docs.R2Bucket,
		Backup:        docsBackup,
		BackupBucket:  cfg.Docs.BackupBucket,
		Members:       pushp.store,
		WarnTotalMB:   cfg.Storage.WarnTotalMB,
		CacheSeconds:  cfg.Storage.CacheSeconds,
		// ⚠ NO Litestream replica dependency, and its absence is a DECISION rather
		// than an omission (PRD §V9-12, settled with Karel 2026-08-24). D214 asked
		// for a replica line, which would mean handing the application process the
		// credentials for the household's entire database backup — no NEW secret,
		// but a real widening of what this binary can reach. Declined. See
		// admin.StorageReplica for what that costs and what it does not.
	}))

	return appModules{
		mountAPI: func(api chi.Router) { registry.MountAll(api, modules) },
		admin:    adminSvc,
		garden:   gardenSvc,
		chat:     chatSvc,
		docs:     docsSvc,
		notes:    notesSvc,
		preview:  previewWorker,
		blob:     docsBlob,
		backup:   docsBackup,
	}, nil
}

// ---------------------------------------------------------------------------
// 5c. Background workers
// ---------------------------------------------------------------------------

// startBackground starts everything that outlives a request: the preview worker,
// the audit outbox tailer, the wall-clock scheduler and the two blob mirror
// jobs. All of them stop with ctx.
//
// It returns the notifier because the shutdown sequence has to JOIN it — see
// serve, where the ordering is the subtle part.
func startBackground(ctx context.Context, cfg *config.Config, sqldb *sql.DB, sink *audit.Writer, app appModules, logger *slog.Logger) *audit.Notifier {
	app.preview.Start(ctx)

	// v5 background workers: the audit outbox tailer (trigger notifications)
	// and the wall-clock scheduler (summaries). These two are the ONLY non-registry
	// wiring v5 adds — everything else registers itself.
	//
	// The tailer turns audit_events into a transactional outbox: it reads only
	// COMMITTED rows, which is how a trigger fires after the change it describes
	// without any module's write path knowing the notifier exists (D56).
	notifier := audit.NewNotifier(sqldb, audit.NewSQLCursor(sqldb), audit.NotifierConfig{Logger: logger})
	notifier.Register(app.admin.Listener())
	// Record nudges the tailer so a trigger fires in about a second rather than
	// waiting for the next poll. It runs inside the writer's transaction, so the
	// nudge is non-blocking by construction.
	sink.SetNudge(notifier.Nudge)
	notifier.Start(ctx)

	sched := scheduler.New(app.admin.Store(), app.admin.FireSchedule, scheduler.Config{
		Location:     cfg.Timezone,
		Tick:         cfg.Notif.SchedTick,
		CatchupGrace: cfg.Notif.CatchupGrace,
		Logger:       logger,
	})
	// v7's ONE platform edit: the garden's forecast poll rides the existing
	// ticker rather than starting a second one inside a feature module — which is
	// precisely what this package was created to avoid. The job is soft: every
	// failure is logged and swallowed, and the module degrades to manual frost
	// dates with nothing user-visible (D112).
	//
	// The module sends NO push. It writes one `garden.frost_warning` audit event
	// whose Czech summary already reads as a finished notification, and publishes
	// garden.frost_risk_tonight / garden.frost_sensitive_now. Whether that becomes
	// a scheduled summary or a trigger rule is chosen in Administrace at runtime
	// (D113) — both work on day one because the module publishes for both.
	if cfg.Garden.WeatherEnabled {
		sched.RegisterJob("garden.weather", time.Duration(cfg.Garden.WeatherPollHours)*time.Hour, app.garden.WeatherJob)
	} else {
		logger.Info("garden: weather poll disabled (HOME_GARDEN_WEATHER_ENABLED=false) — manual frost dates only")
	}
	// v10's ONE scheduler job, and it is chat's only reason for taking
	// platform/scheduler at all (D247). Every path in chat that destroys bytes
	// enqueues into chat_deleted_keys with a purge_after; this drains what is due,
	// then purges the conversations whose koš window has elapsed — in that order,
	// inside one job, so the bytes go before the rows that pointed at them.
	//
	// ⚠ THE CLEAN-UP PAGE IS THE EXCEPTION AND DELETES INLINE. The workflow there is
	// *clean until the number goes down*, and a figure lagging fifteen minutes behind
	// the button makes the page unusable — while a conversation with four hundred
	// attachments is exactly the case that must not block a request.
	//
	// ⚠ THE JOB HAS NO ACTOR, which is the hazard §V10-4a names: every load it makes
	// is an explicit any-membership variant (chat/drain.go).
	sched.RegisterJob("chat.drain", chat.DrainInterval, app.chat.DrainJob)
	sched.Start(ctx)

	// Deliveries are operational, not audit (D64): prune them on boot, the same
	// way the audit retention pass runs, so a long-lived household does not
	// accumulate a delivery row per device per notification forever.
	if pruned, err := app.admin.PruneDeliveries(context.Background(), cfg.Notif.DeliveryRetentionDays); err != nil {
		logger.Warn("admin: prune deliveries", "err", err)
	} else if pruned > 0 {
		logger.Info("admin: pruned delivery log", "retention_days", cfg.Notif.DeliveryRetentionDays, "pruned", pruned)
	}

	startMirrors(ctx, cfg, app, logger)
	return notifier
}

// startMirrors runs the documents and note-image mirror/reconciliation jobs.
//
// The two run on IDENTICAL settings — the same three cfg.Docs values, the same
// backup, the same logger — because note images live in the documents bucket
// under their own prefix and share its backup. The three derived values are
// computed once here so the pair cannot drift into two cadences by way of one
// edit.
//
// ⚠ THE TWO CONFIG LITERALS STAY SEPARATE. documents.MirrorConfig and
// notes.ImageMirrorConfig are field-identical but they are DIFFERENT TYPES,
// one per module — which is `notes/mirror.go`'s recorded point, that a change
// to one module's storage must not be able to quietly alter the other's.
func startMirrors(ctx context.Context, cfg *config.Config, app appModules, logger *slog.Logger) {
	mirrorInterval := cfg.Docs.MirrorInterval
	orphanGrace := time.Duration(cfg.Docs.OrphanGraceHours) * time.Hour
	maxOrphanShare := float64(cfg.Docs.OrphanMaxPercent) / 100

	documents.NewMirrorJob(app.docs.Store(), app.blob, documents.MirrorConfig{
		Interval:       mirrorInterval,
		OrphanGrace:    orphanGrace,
		MaxOrphanShare: maxOrphanShare,
		Backup:         app.backup,
		Logger:         logger,
	}).Run(ctx)
	// Note images share the documents bucket + backup, mirrored/reconciled on the same
	// cadence but scoped to the note-images/ prefix and the note_images table.
	notes.NewImageMirrorJob(app.notes.Store(), app.blob, notes.ImageMirrorConfig{
		Interval:       mirrorInterval,
		OrphanGrace:    orphanGrace,
		MaxOrphanShare: maxOrphanShare,
		Backup:         app.backup,
		Logger:         logger,
	}).Run(ctx)
}

// ---------------------------------------------------------------------------
// 7. Serve and shut down
// ---------------------------------------------------------------------------

// shutdownDeps is what the two-phase shutdown has to reach. It exists so the
// ordering below reads as one screen: every line of it is load-bearing, and the
// comments explaining why are longer than the code.
type shutdownDeps struct {
	stopBackground func()
	preview        *documents.PreviewWorker
	notifier       *audit.Notifier
	admin          *admin.Service
}

// serve listens until interrupted, then shuts down gracefully.
func serve(cfg *config.Config, logger *slog.Logger, handler http.Handler, sd shutdownDeps) error {
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

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
		// Stop the background workers first so an in-flight preview conversion is
		// cancelled rather than killed mid-write.
		sd.stopBackground()
		sd.preview.Wait()
		// JOIN the outbox tailer before the flush below, don't just cancel it.
		// stopBackground only asks it to stop: it can still be part way through a
		// batch, and every event left in that batch is handed to the admin
		// listener first — which may start another send. FlushPending waits on
		// the sends already in flight, so a dispatch racing that wait would
		// either be abandoned mid-send (the notification is lost: its event's
		// cursor has already advanced) or trip the WaitGroup's own misuse check.
		// The join is bounded — with the context cancelled every query in the
		// tailer fails at once.
		sd.notifier.Wait()
		// Then send whatever is still sitting in a coalescing window. The outbox
		// cursor has already moved past those events, so they will never be
		// redelivered — a window killed by a deploy loses its notification rather
		// than delaying it.
		//
		// CONCURRENTLY with draining HTTP, not before it: the two are independent,
		// and running them in sequence would put the worst case at 15s — past the
		// 10s Docker allows between SIGTERM and SIGKILL, which would cut both
		// short. Overlapped, the whole shutdown still fits in the grace period.
		flushCtx, cancelFlush := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelFlush()
		flushed := make(chan struct{})
		go func() {
			defer close(flushed)
			sd.admin.FlushPending(flushCtx)
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := srv.Shutdown(ctx)
		<-flushed
		return err
	}
}

// wsRevalidation translates auth's session verdict into the websocket hub's.
//
// ⚠ It is a named function rather than an inline switch because it is the one
// place the two three-state vocabularies meet, and inverting a single arm here is
// invisible: mapping SessionLive to RevalidationGone tears down every socket in
// the household on the first healthy tick, and mapping the default arm to
// RevalidationGone does the same on the first contended query — with the whole
// backend suite still green, because nothing else crosses this boundary.
// TestWSRevalidation pins all three.
//
// The default arm is deliberately RevalidationUnknown: an unrecognised verdict is
// a decision that could not be taken, and the safe direction for one of those is
// keeping live boards up.
func wsRevalidation(verdict auth.SessionVerdict) ws.Revalidation {
	switch verdict {
	case auth.SessionLive:
		return ws.RevalidationValid
	case auth.SessionGone:
		return ws.RevalidationGone
	default:
		return ws.RevalidationUnknown
	}
}

// openDocumentStores builds the primary (and optional backup) object store for the
// documents module. Production always uses R2 — config refuses to start otherwise —
// while development falls back to a local directory so the app runs with no cloud
// credentials at all.
func openDocumentStores(cfg *config.Config, logger *slog.Logger) (primary, backup blobstore.BlobStore, err error) {
	d := cfg.Docs
	if d.UsesObjectStorage() {
		s3, err := blobstore.NewS3(blobstore.S3Config{
			Bucket:    d.R2Bucket,
			Endpoint:  d.R2Endpoint,
			AccessKey: d.R2AccessKeyID,
			SecretKey: d.R2SecretAccessKey,
		})
		if err != nil {
			return nil, nil, err
		}
		primary = s3
		logger.Info("documents: object storage ready", "bucket", d.R2Bucket, "endpoint", d.R2Endpoint)
	} else {
		fs, err := blobstore.NewFS(d.LocalDir)
		if err != nil {
			return nil, nil, err
		}
		primary = fs
		logger.Warn("documents: using the LOCAL FILESYSTEM store — no backup coverage; development only",
			"dir", d.LocalDir)
	}

	if d.BackupBucket != "" {
		b, err := blobstore.NewS3(blobstore.S3Config{
			Bucket:    d.BackupBucket,
			Endpoint:  d.BackupEndpoint,
			AccessKey: d.BackupAccessKeyID,
			SecretKey: d.BackupSecretAccessKey,
		})
		if err != nil {
			return nil, nil, err
		}
		backup = b
	}
	return primary, backup, nil
}

// toAny widens the module slice for the catalog collectors, which take `any`
// because their interfaces are OPTIONAL — a module that owns no table simply does
// not implement storage.Source, and the collector type-asserts rather than
// forcing every module to satisfy a contract it has no use for (the D56 rule).
func toAny(modules []registry.Module) []any {
	out := make([]any, len(modules))
	for i, m := range modules {
		out[i] = m
	}
	return out
}
