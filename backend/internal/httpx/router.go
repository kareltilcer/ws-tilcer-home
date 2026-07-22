package httpx

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Deps carries everything the router needs. It grows as modules land (websocket
// hub, feature handlers) without churning call sites.
type Deps struct {
	Logger       *slog.Logger
	DB           Pinger
	Site         string
	InsecureAuth bool
	Auth         AuthConfig
	// WS is the authenticated websocket upgrade handler (F5). It authenticates
	// itself (a browser cannot send an Authorization header on a websocket), so
	// it is mounted outside the /api auth group.
	WS http.Handler
	// MountAPI mounts feature subrouters onto the authenticated /api group. Each
	// module composes its own routes here (applying RequireWrite/RequireAdmin as
	// needed). Runs after Authenticate.
	MountAPI func(api chi.Router)
	// StaticDir, when non-empty, is the directory of the built SPA served on all
	// non-API routes with an index.html fallback. Empty in development (Vite
	// serves the SPA and proxies /api + /ws to Go).
	StaticDir string
}

// NewRouter assembles the HTTP handler: baseline middleware, public health
// probes, and (in later phases) the authenticated /api, /ws, and the SPA
// fallback. Health probes are intentionally mounted OUTSIDE the request-id/log
// stack's auth so they stay public and cheap.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(RequestID(d.Site))
	r.Use(Logger(d.Logger))
	r.Use(Recover(d.Logger))

	// Health (public).
	r.Get("/healthz", Healthz())
	r.Get("/readyz", Readyz(d.DB, d.InsecureAuth))

	// Authenticated API surface. Reads are open to any authenticated user;
	// feature subrouters gate their mutations with RequireWrite, and the log
	// browser with RequireAdmin. Feature modules mount here in later phases.
	r.Route("/api", func(api chi.Router) {
		api.Use(Authenticate(d.Auth))
		if d.MountAPI != nil {
			d.MountAPI(api)
		}
	})

	// Websocket (self-authenticating).
	if d.WS != nil {
		r.Handle("/ws", d.WS)
	}

	// Catch-all: paths that matched no route above land here. The built SPA is
	// served on non-API routes with an index.html fallback for client-side
	// routes. An unmatched /api/** path or /ws must NOT fall through to the SPA
	// shell — it gets a JSON 404 so a mistyped endpoint fails loudly. Registered
	// last so chi propagates this handler onto the /api subrouter too.
	var spa http.Handler
	if d.StaticDir != "" {
		spa = SPAHandler(d.StaticDir)
	}
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if spa != nil && !strings.HasPrefix(req.URL.Path, "/api/") && req.URL.Path != "/ws" {
			spa.ServeHTTP(w, req)
			return
		}
		WriteError(w, ErrNotFound("no such endpoint"))
	})

	return r
}
