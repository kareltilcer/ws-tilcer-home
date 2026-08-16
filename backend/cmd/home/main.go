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
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/admin"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/dashboard"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/documents"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
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

	// 3b. The shared Web Push channel (v5, D52). One service worker on one origin
	// ⇒ one subscription per device ⇒ one channel every module sends through. It
	// is platform infrastructure, not the admin module: a member's consent and
	// mutes must outlive whatever module wants to notify them (D53).
	//
	// With no VAPID keypair the channel is inert — subscribing is refused with a
	// clear reason and nothing is ever sent — so the whole app still runs.
	pushStore := push.NewStore(sqldb)
	pushSvc := push.NewService(pushStore, push.Config{
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
	if pushSvc.Enabled() {
		logger.Info("push: channel ready", "subject", cfg.Notif.VAPIDSubject)
	} else {
		logger.Warn("push: DISABLED — no VAPID keypair configured; notifications will not be sent " +
			"(set HOME_VAPID_PUBLIC_KEY / HOME_VAPID_PRIVATE_KEY / HOME_VAPID_SUBJECT; generate with `go run ./cmd/vapidgen`)")
	}
	pushHandler := push.NewHandler(pushSvc, sqldb, sink)

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
	// Modules publish websocket change events via the hub after commit. The push
	// carries the originating request's client id (from reqctx) so each browser
	// tab can tell its own echo apart from a change made on another device.
	notify := func(ctx context.Context, typ string, payload any) {
		origin := ""
		if info, ok := reqctx.RequestFrom(ctx); ok {
			origin = info.ClientID
		}
		hub.Publish(ws.Message{Type: typ, Origin: origin, Payload: payload})
	}

	todoSvc := todo.NewService(sqldb, sink, notify)
	eventsSvc := events.NewService(sqldb, sink, notify, cfg.RRuleMaxOccurrences, cfg.RRuleMaxWindowMonths)

	// documents (v4) is the first module with bytes outside SQLite: it needs an object
	// store, an async preview worker, and — because Litestream cannot back up a blob
	// bucket — its own mirror/reconciliation job (D45).
	docsBlob, docsBackup, err := openDocumentStores(cfg, logger)
	if err != nil {
		return err
	}
	docsSvc := documents.NewService(sqldb, sink, notify, docsBlob, documents.Options{
		MaxUploadBytes: int64(cfg.Docs.MaxUploadMB) << 20,
		AllowedMIME:    cfg.Docs.AllowedMIME,
		PreviewEnabled: cfg.Docs.PreviewEnabled,
		PublicBaseURL:  cfg.Docs.PublicBaseURL,
	}, logger)

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

	// The dashboard host renders widgets contributed by the feature modules — it
	// reaches feature data only through this catalog, never their tables (D28).
	catalog, err := registry.NewCatalog(registry.CollectWidgets([]registry.Module{todoMod, eventsMod, notesMod, docsMod}))
	if err != nil {
		return err
	}
	dashMod := dashboard.NewModule(catalog, sqldb)

	// 5b. v5: the metrics catalog — the THIRD registered catalog, beside widgets
	// and audit actions. Modules publish counts; the admin module's summaries
	// reference them by key and never touch a feature table (D59/D28).
	featureModules := []registry.Module{loggingMod, todoMod, eventsMod, notesMod, docsMod, dashMod}
	metricRegistry, err := metrics.Collect(todoMod, eventsMod, notesMod, docsMod)
	if err != nil {
		return err
	}
	// The list catalog beside it: the same modules also publish WHICH items are
	// behind those counts, so a summary can name today's reminders instead of
	// only counting them. Collected the same way, read through the same kind of
	// registry, so admin still imports no feature module.
	listRegistry, err := lists.Collect(todoMod, eventsMod, notesMod, docsMod)
	if err != nil {
		return err
	}
	// The action catalog the trigger composer picks from: every module's declared
	// verbs plus the platform-emitted ones (login, push.*), which belong to no
	// module and would otherwise be unbindable.
	actions := registry.CollectActions(featureModules)
	for _, key := range audit.PlatformActions {
		actions = append(actions, registry.Action{Key: key, Module: audit.ModulePlatform})
	}

	adminSvc := admin.NewService(sqldb, sink, admin.Options{
		Sender:          pushSvc,
		PushStore:       pushStore,
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
	pushSvc.SetRecorder(adminSvc.Store())

	modules := append(featureModules, adminMod)
	mountAPI := func(api chi.Router) {
		// /api/push/** is platform, not a module: every member (reader included)
		// manages their own device here, whether or not any module sends anything.
		pushHandler.Mount(api)
		registry.MountAll(api, modules)
	}

	// Background workers live for the process's lifetime and stop with this context,
	// which is cancelled on the shutdown signal below.
	bgCtx, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	previewWorker.Start(bgCtx)

	// 5c. v5 background workers: the audit outbox tailer (trigger notifications)
	// and the wall-clock scheduler (summaries). These two are the ONLY non-registry
	// wiring v5 adds — everything else registers itself.
	//
	// The tailer turns audit_events into a transactional outbox: it reads only
	// COMMITTED rows, which is how a trigger fires after the change it describes
	// without any module's write path knowing the notifier exists (D56).
	notifier := audit.NewNotifier(sqldb, audit.NewSQLCursor(sqldb), audit.NotifierConfig{Logger: logger})
	notifier.Register(adminSvc.Listener())
	// Record nudges the tailer so a trigger fires in about a second rather than
	// waiting for the next poll. It runs inside the writer's transaction, so the
	// nudge is non-blocking by construction.
	sink.SetNudge(notifier.Nudge)
	notifier.Start(bgCtx)

	scheduler.New(adminSvc.Store(), adminSvc.FireSchedule, scheduler.Config{
		Location:     cfg.Timezone,
		Tick:         cfg.Notif.SchedTick,
		CatchupGrace: cfg.Notif.CatchupGrace,
		Logger:       logger,
	}).Start(bgCtx)

	// Deliveries are operational, not audit (D64): prune them on boot, the same
	// way the audit retention pass runs, so a long-lived household does not
	// accumulate a delivery row per device per notification forever.
	if pruned, err := adminSvc.PruneDeliveries(context.Background(), cfg.Notif.DeliveryRetentionDays); err != nil {
		logger.Warn("admin: prune deliveries", "err", err)
	} else if pruned > 0 {
		logger.Info("admin: pruned delivery log", "retention_days", cfg.Notif.DeliveryRetentionDays, "pruned", pruned)
	}

	documents.NewMirrorJob(docsSvc.Store(), docsBlob, documents.MirrorConfig{
		Interval:       cfg.Docs.MirrorInterval,
		OrphanGrace:    time.Duration(cfg.Docs.OrphanGraceHours) * time.Hour,
		MaxOrphanShare: float64(cfg.Docs.OrphanMaxPercent) / 100,
		Backup:         docsBackup,
		Logger:         logger,
	}).Run(bgCtx)
	// Note images share the documents bucket + backup, mirrored/reconciled on the same
	// cadence but scoped to the note-images/ prefix and the note_images table.
	notes.NewImageMirrorJob(notesSvc.Store(), docsBlob, notes.ImageMirrorConfig{
		Interval:       cfg.Docs.MirrorInterval,
		OrphanGrace:    time.Duration(cfg.Docs.OrphanGraceHours) * time.Hour,
		MaxOrphanShare: float64(cfg.Docs.OrphanMaxPercent) / 100,
		Backup:         docsBackup,
		Logger:         logger,
	}).Run(bgCtx)

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
		// Stop the background workers first so an in-flight preview conversion is
		// cancelled rather than killed mid-write.
		stopBackground()
		previewWorker.Wait()
		// JOIN the outbox tailer before the flush below, don't just cancel it.
		// stopBackground only asks it to stop: it can still be part way through a
		// batch, and every event left in that batch is handed to the admin
		// listener first — which may start another send. FlushPending waits on
		// the sends already in flight, so a dispatch racing that wait would
		// either be abandoned mid-send (the notification is lost: its event's
		// cursor has already advanced) or trip the WaitGroup's own misuse check.
		// The join is bounded — with the context cancelled every query in the
		// tailer fails at once.
		notifier.Wait()
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
			adminSvc.FlushPending(flushCtx)
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := srv.Shutdown(ctx)
		<-flushed
		return err
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
