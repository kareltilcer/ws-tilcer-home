package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcpctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The front door (v11, PRD §V11-4 FR-M1, D277–D282).
//
// ⚠ IT IS MOUNTED OUTSIDE THE /api GROUP, and every layer that group provides is
// decided here rather than inherited (D279). Inside it, SessionMW would make a
// cookie sufficient — leak row 1, and the endpoint would be the only
// cookie-authenticated, CSRF-unprotected one in Home, drivable cross-origin by
// any website a member visits — and CSRFMW would reject every call, since an MCP
// client sends no X-CSRF-Token.

// maxBodyBytes bounds an inbound envelope. A tool's ARGUMENTS are small by
// construction (there is no upload tool, D295), so this is generous for anything
// legitimate and cheap protection for the one method that needs no bearer.
const maxBodyBytes = 1 << 20 // 1 MiB

// Mount installs the MCP endpoint. The caller mounts it at /mcp, OUTSIDE /api.
func (h *Host) Mount(r chi.Router) {
	// ⚠ HOME_MCP_ENABLED FALSE ⇒ 404, NOT 403. A disabled feature should look
	// ABSENT: a 403 says "this exists and you may not have it", which sends the
	// person reading it after a permission problem that does not exist.
	if !h.deps.Config.Enabled {
		return
	}
	r.Post("/", h.serve)
	// ⚠ GET IS 405 WITH AN Allow HEADER, NOT 404 (D277). v11 sends no
	// server-initiated notifications and offers no resource subscriptions, so the
	// SSE stream a client opens with GET does not exist — and a client told 405
	// stops asking, where one told 404 concludes the endpoint is wrong.
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"POST only — this server sends no server-initiated notifications.")
	})
	// ⚠ AND AN UNMATCHED /mcp PATH IS JSON, NOT THE SPA. The router's own NotFound
	// excludes /mcp (leak row 2), but chi propagates a NotFound registered on a
	// subrouter, so this is the second half of that guard and the one a mistyped
	// path actually hits.
	//
	// ⚠ IT IS REGISTERED ONLY WHEN THE SERVER IS ENABLED, AND THE DISABLED DOOR IS
	// STILL JSON — checked rather than assumed, because chi's Mount() copies the
	// parent's NotFound onto a subrouter only if the parent HAS one at Mount()
	// time, and httpx.NewRouter registers its own afterwards. What closes the gap
	// is the OTHER half of chi's contract: Mux.NotFound walks the subroutes it has
	// already mounted and installs the handler on any that lack one, so the empty
	// /mcp sub-mux a disabled server leaves behind inherits the router's JSON 404
	// — the one that excludes /mcp from the SPA. TestDisabledServerIs404 asserts
	// the Content-Type, not merely the status, so this stays true by test rather
	// than by reading chi.
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		writeJSONError(w, http.StatusNotFound, "not_found", "no such MCP endpoint")
	})
}

// serve handles one JSON-RPC call.
//
// ⚠ THE CHAIN IS HANDOFF-13 §4.3's, WITH ONE STRUCTURAL DEVIATION STATED HERE
// RATHER THAN DISCOVERED. Steps 1–3 (enabled, method, Origin) are decidable from
// the request line and headers alone. Steps 4–7 (bearer, rate limit, semaphore,
// deadline) are NOT: `initialize` is exempt from the bearer (D312), and both the
// limiter and the semaphore are keyed by a token id that does not exist until the
// bearer resolves — so the envelope is read first. The order among 4–7 is
// preserved exactly, and the unauthenticated methods get their own IP-keyed
// limiter so the exemption is not a free hole.
func (h *Host) serve(w http.ResponseWriter, r *http.Request) {
	// ⚠ THE HEADER NAMES WHAT HOME SPEAKS, NEVER WHAT THE CALLER ASKED FOR. It
	// used to echo the request's value back unread, so a client pinned to a
	// revision home does not implement was served in full and told, in the
	// negotiated-version header, that home spoke it too. Home speaks exactly one
	// (D278) and says so on every answer.
	w.Header().Set(protocolVersionHeader, protocolVersion)
	if v := r.Header.Get(protocolVersionHeader); v != "" && v != protocolVersion {
		writeJSONError(w, http.StatusBadRequest, "unsupported_protocol_version",
			"unsupported MCP-Protocol-Version "+v+"; home speaks "+protocolVersion)
		return
	}

	// 3. Origin (D281). ⚠ A request claiming NO origin is allowed — a CLI has none
	// — but only on a valid bearer, which step 4 then requires. A request that
	// DOES claim one must be on the allowlist: this is the DNS-rebinding defence
	// the MCP specification requires of an HTTP server, and it reuses CSRF's own
	// allowlist parser rather than growing a second one.
	if auth.RequestOrigin(r) != "" && !auth.OriginAllowed(r, h.deps.Config.AllowedOrigins) {
		writeJSONError(w, http.StatusForbidden, "forbidden", "origin not allowed")
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "expected application/json")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeRPC(w, http.StatusOK, newError(nil, CodeParseError, "request body could not be read"))
		return
	}
	// ⚠ A BATCH IS REFUSED, NOT PARTIALLY HONOURED. The 2025-06-18 revision removed
	// batching; processing the first element of an array would leave the client
	// believing five calls ran.
	if isBatch(body) {
		writeRPC(w, http.StatusOK, newError(nil, CodeInvalidRequest,
			"JSON-RPC batching is not supported; send one request per POST"))
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPC(w, http.StatusOK, newError(nil, CodeParseError, "malformed JSON-RPC request"))
		return
	}
	if req.Method == "" {
		writeRPC(w, http.StatusOK, newError(req.ID, CodeInvalidRequest, "missing method"))
		return
	}

	ctx := r.Context()
	s := &callSession{}

	ip := clientIP(r)

	if bearer := bearerToken(r); bearer != "" || needsBearer(req.Method) {
		// 4. Bearer. ⚠ THE SESSION COOKIE IS NOT READ HERE AT ALL, not even as a
		// fallback (D280) — a cookie-only request is 401, never "authenticated as
		// the cookie's owner". ⚠ And a bearer that is PRESENT must resolve even for
		// an exempt method: a caller holding a dead credential is told so once,
		// rather than being quietly served the anonymous answer and left to wonder.
		//
		// ⚠ A BEARER THAT DOES NOT RESOLVE IS KEYED BY NOTHING BUT THE IP, so its
		// budget is checked HERE, before the lookup, and spent only on failure.
		// Resolve's first act is an indexed SELECT on the ONE database connection;
		// the per-token limiter below cannot bound it, because there is no token id
		// until it succeeds. Without this a stranger posting junk bearers holds the
		// connection the household's browser is queued on, and the anon limiter
		// never sees them — it is in the branch below, which a request carrying an
		// Authorization header never reaches.
		if bearer != "" && h.fails.blocked(ip) {
			writeJSONError(w, http.StatusTooManyRequests, "rate_limited", "too many calls; slow down")
			return
		}
		principal, err := h.deps.Auth.Resolve(ctx, bearer, ip)
		if err != nil {
			if !errors.Is(err, auth.ErrTokenNotFound) {
				// A store failure is not an authentication verdict, and reporting it as
				// one would tell a caller their token is dead when the database is.
				h.deps.Logger.Error("mcp token lookup failed", "err", err)
				writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "authentication is temporarily unavailable")
				return
			}
			// Only a FAILURE spends the IP budget — a working client never touches it,
			// so this cannot throttle the household's own assistant.
			h.fails.record(ip)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "a valid MCP bearer token is required")
			return
		}
		s.principal = principal
		s.actor = principal.Actor()

		// 5. Rate limit, per token id (D306).
		if !h.calls.allow(principal.Token.ID) {
			writeJSONError(w, http.StatusTooManyRequests, "rate_limited", "too many calls; slow down")
			return
		}

		ctx = reqctx.WithActor(ctx, s.actor)
		ctx = mcpctx.WithToken(ctx, principal.Token.ID)

		// 6. Semaphore, then 7. deadline — IN THAT ORDER. ⚠ Acquiring first is
		// load-bearing: a call that queued behind two others and only then started
		// its 20-second clock would spend its whole budget waiting, and time out
		// having done nothing.
		release, err := h.sems.acquire(ctx, principal.Token.ID)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "the request was cancelled while queued")
			return
		}
		defer release()

		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.deps.Config.CallTimeout)
		defer cancel()
	} else if !h.anon.allow(ip) {
		// The bearer-exempt methods are cheap — `initialize` is a map literal and
		// `notifications/initialized` does nothing at all — but "cheap" is not
		// "free", so they are limited by IP rather than left unbounded.
		writeJSONError(w, http.StatusTooManyRequests, "rate_limited", "too many calls; slow down")
		return
	}

	// A notification carries no id and MUST NOT be answered with a response
	// object. `notifications/initialized` is the one every client sends.
	if req.isNotification() {
		if _, err := h.dispatch(ctx, s, req.Method, req.Params); err != nil {
			h.deps.Logger.Warn("mcp notification failed", "method", req.Method, "err", err)
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	result, err := h.dispatch(ctx, s, req.Method, req.Params)
	if err != nil {
		var pe *ProtocolError
		if errors.As(err, &pe) {
			writeRPC(w, http.StatusOK, newError(req.ID, pe.Code, pe.Message))
			return
		}
		// ⚠ AN INTERNAL FAILURE IS LOGGED AND ANSWERED WITHOUT DETAIL. It reaches
		// the crash board through the slog handler, under a request id, where a
		// person can read it — and the wire gets nothing describing the database.
		h.logTool(ctx, "mcp dispatch failed", req.Method, err)
		writeRPC(w, http.StatusOK, newError(req.ID, CodeInternalError, internalText))
		return
	}
	writeRPC(w, http.StatusOK, newResult(req.ID, result))
}

// bearerToken reads the Authorization header.
//
// ⚠ THE RETURNED VALUE IS A SECRET AND IS NEVER LOGGED — not here, not by
// httpx.Logger (which logs a method, a path, a status and a request id, and no
// headers at all), and not by httpx.Recover (which logs the panic, the path, the
// request id and the stack). That is the whole of leak row 14: the token never
// leaves this function's caller, so nothing can lift it into a crash report.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// clientIP mirrors httpx's own extraction: Coolify terminates TLS and proxies, so
// X-Forwarded-For is the real client. It is read from reqctx when the root chain
// has already resolved it, which it always has in production.
func clientIP(r *http.Request) string {
	if info, ok := reqctx.RequestFrom(r.Context()); ok && info.IP != "" {
		return info.IP
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	return r.RemoteAddr
}

// writeRPC writes one JSON-RPC response.
//
// ⚠ A JSON-RPC ERROR IS AN HTTP 200. The transport succeeded; the call did not.
// Answering 400 would make an HTTP-level retry look reasonable to a client that
// should instead read the error object.
func writeRPC(w http.ResponseWriter, status int, resp rpcResponse) {
	httpx.JSON(w, status, resp)
}

// writeJSONError writes a transport-level refusal in Home's shared envelope.
//
// ⚠ ALWAYS application/json, NEVER HTML. Leak row 2's test asserts on the
// Content-Type rather than the status, because the failure it guards against —
// index.html with a 200 — looks like a working server from the client's side.
func writeJSONError(w http.ResponseWriter, status int, code, detail string) {
	httpx.JSON(w, status, httpx.Error{Err: code, Detail: detail})
}
