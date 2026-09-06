package httpx

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Deps carries everything the router needs. It grows as modules land without
// churning call sites.
type Deps struct {
	Logger       *slog.Logger
	DB           Pinger
	Site         string
	InsecureAuth bool
	// MountAuth mounts the public /api/auth/* endpoints (login/logout/session).
	// These run OUTSIDE the session gate — login must work before a session exists
	// (Mode B, FR-A1). May be nil.
	MountAuth func(api chi.Router)
	// SessionMW authorizes every gated /api request from the home session cookie
	// (FR-A2). Applied to the gated group only. May be nil (tests).
	SessionMW func(http.Handler) http.Handler
	// CSRFMW enforces the double-submit CSRF check on cookie-authenticated
	// mutations within the gated group (FR-A5). May be nil (tests).
	CSRFMW func(http.Handler) http.Handler
	// MountAPI mounts feature modules onto the authenticated, CSRF-protected /api
	// group. Each module composes its own routes here (applying RequireWrite /
	// RequireAdmin as needed).
	MountAPI func(api chi.Router)
	// WS is the session-authenticated websocket upgrade handler (F5). It
	// authenticates itself from the session cookie, so it is mounted outside the
	// /api group.
	WS http.Handler
	// MountMCP mounts the v11 MCP front door at /mcp. May be nil.
	//
	// ⚠ IT IS MOUNTED OUTSIDE THE /api GROUP AND MUST STAY THERE (D279). Inside,
	// it would inherit SessionMW — making a cookie sufficient, which is leak row 1
	// and would leave Home with one cookie-authenticated endpoint outside the CSRF
	// double-submit check, drivable cross-origin by any website a member visits —
	// and CSRFMW, which would reject every call, since an MCP client sends no
	// X-CSRF-Token. It brings its own chain: bearer, Origin, rate limit,
	// semaphore, deadline.
	MountMCP func(r chi.Router)
	// StaticDir, when non-empty, is the directory of the built SPA served on all
	// non-API routes with an index.html fallback.
	StaticDir string
}

// NewRouter assembles the HTTP handler: baseline middleware, public health
// probes, the public auth endpoints, the session-gated + CSRF-protected /api
// surface, the websocket, and the SPA fallback. Health probes are mounted
// OUTSIDE auth so they stay public and cheap.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(RequestID(d.Site))
	r.Use(Logger(d.Logger))
	r.Use(Recover(d.Logger))

	// Health (public).
	r.Get("/healthz", Healthz())
	r.Get("/readyz", Readyz(d.DB, d.InsecureAuth))

	r.Route("/api", func(api chi.Router) {
		// Public auth endpoints (login is pre-session, Mode B).
		if d.MountAuth != nil {
			d.MountAuth(api)
		}
		// Everything else is authorized from the session cookie and, for
		// mutations, CSRF-protected.
		api.Group(func(gated chi.Router) {
			if d.SessionMW != nil {
				gated.Use(d.SessionMW)
			}
			if d.CSRFMW != nil {
				gated.Use(d.CSRFMW)
			}
			if d.MountAPI != nil {
				d.MountAPI(gated)
			}
		})
	})

	// Websocket (self-authenticating from the session cookie).
	if d.WS != nil {
		r.Handle("/ws", d.WS)
	}

	// The MCP front door (v11), on its own middleware chain — see Deps.MountMCP
	// for why it is here and not inside /api.
	if d.MountMCP != nil {
		r.Route("/mcp", d.MountMCP)
	}

	// Catch-all: the built SPA is served on non-API routes with an index.html
	// fallback for client-side routes. An unmatched /api/** path, /ws or /mcp must
	// NOT fall through to the SPA shell — it gets a JSON 404 so a mistyped endpoint
	// fails loudly. Registered last so chi propagates this onto /api too.
	//
	// ⚠ /mcp JOINED THIS LIST IN v11 AND IT IS THE FIRST THING THAT WOULD HAVE
	// BEEN GOT WRONG (D282, leak row 2). Without it a mistyped MCP path returns
	// index.html WITH A 200, and the client reports a protocol error against a
	// perfectly healthy server. ⚠ It has an identical twin that is not code: if the
	// Coolify path route for /mcp is missing, the request never reaches this binary
	// and the FRONTEND's Nginx returns index.html with a 200 — same symptom,
	// different layer. When /mcp returns HTML, check both, in that order: curl the
	// container directly, then the origin.
	var spa http.Handler
	if d.StaticDir != "" {
		spa = SPAHandler(d.StaticDir)
	}
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if spa != nil && !strings.HasPrefix(req.URL.Path, "/api/") &&
			req.URL.Path != "/ws" && !strings.HasPrefix(req.URL.Path, "/mcp") {
			spa.ServeHTTP(w, req)
			return
		}
		WriteError(w, ErrNotFound("no such endpoint"))
	})

	return r
}
