package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Login rate-limit (FR-A5): per email and per IP.
const (
	loginMaxAttempts = 10
	loginWindow      = 15 * time.Minute
)

// Handler serves the home-hosted /api/auth endpoints (Mode B, FR-A1/A3/A4).
type Handler struct {
	cfg     Config
	db      *sql.DB
	sink    audit.Sink
	limiter *rateLimiter
}

// NewHandler builds the auth handler. sink records platform.login / platform.logout
// atomically with the session write.
func NewHandler(cfg Config, db *sql.DB, sink audit.Sink) *Handler {
	return &Handler{cfg: cfg, db: db, sink: sink, limiter: newRateLimiter(loginMaxAttempts, loginWindow, cfg.Now)}
}

// Mount registers the auth routes on the /api router (OUTSIDE the session gate —
// login must work before there is a session). csrf is applied to logout only;
// login is CSRF-exempt but rate-limited, and the session probe is a safe GET.
func (h *Handler) Mount(api chi.Router, csrf func(http.Handler) http.Handler) {
	api.Post("/auth/login", h.login)
	api.Get("/auth/session", h.session)
	api.With(csrf).Post("/auth/logout", h.logout)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userPublic struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	DisplayName *string  `json:"display_name"`
	Roles       []string `json:"roles"`
}

type sessionUser struct {
	User userPublic `json:"user"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}

	// Under the dev bypass there is no session store or auth service (both nil):
	// accept the login and echo the bypass actor so the login screen is exercisable
	// offline. Return before any rate-limit or Sessions access.
	if h.cfg.BypassActor != nil {
		a := h.cfg.BypassActor
		id := Identity{UserID: a.UserID, Email: firstNonEmpty(in.Email, a.Label, a.UserID), DisplayName: a.Label, Roles: a.Roles}
		httpx.JSON(w, http.StatusOK, sessionUser{User: publicUser(id)})
		return
	}

	ip := clientIP(r)
	// Rate-limit FAILED logins only (FR-A5): an IP-wide counter caps password
	// spraying across accounts, and a per-(IP, email) counter caps brute force on a
	// single account. Keying the account counter to the IP means one attacker can't
	// lock a victim out from their own network, and only failures count so a user's
	// own repeated sign-ins never trip the limit.
	ipKey := "ip:" + ip
	acctKey := ipKey + "|email:" + normalizeEmail(in.Email)
	if !h.limiter.allowed(ipKey) || !h.limiter.allowed(acctKey) {
		httpx.WriteError(w, &httpx.APIError{Status: http.StatusTooManyRequests, Code: "too_many_requests", Detail: "příliš mnoho pokusů"})
		return
	}

	id, err := h.cfg.Authr.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		if errors.Is(err, ErrBadCredentials) {
			h.limiter.fail(ipKey)
			h.limiter.fail(acctKey)
		}
		writeLoginError(w, err)
		return
	}
	h.limiter.reset(ipKey)
	h.limiter.reset(acctKey)

	now := h.cfg.now()
	req, _ := reqctx.RequestFrom(r.Context())
	// Set the acting user in context so the audit sink stamps the right actor.
	actorCtx := reqctx.WithActor(r.Context(), reqctx.Actor{UserID: id.UserID, Type: "user", Label: labelForIdentity(id), Roles: id.Roles})

	var rawToken string
	if err := appdb.WithTx(actorCtx, h.db, func(tx *sql.Tx) error {
		raw, _, err := h.cfg.Sessions.Create(actorCtx, tx, id, req.UserAgent, ip, h.cfg.SessionTTL, now)
		if err != nil {
			return err
		}
		rawToken = raw
		_, err = h.sink.Record(actorCtx, tx, audit.Event{
			Module: audit.ModulePlatform, Action: "login",
			Summary: fmt.Sprintf("Přihlášení uživatele %s", id.Email),
		})
		return err
	}); err != nil {
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}

	csrfToken, err := newCSRFToken()
	if err != nil {
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}
	setAuthCookies(w, rawToken, csrfToken, h.cfg.SessionTTL, h.cfg.Secure)
	httpx.JSON(w, http.StatusOK, sessionUser{User: publicUser(id)})
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request) {
	if h.cfg.BypassActor != nil {
		a := h.cfg.BypassActor
		httpx.JSON(w, http.StatusOK, sessionUser{User: publicUser(Identity{
			UserID: a.UserID, Email: a.Label, DisplayName: a.Label, Roles: a.Roles,
		})})
		return
	}
	c, err := r.Cookie(cookieSession)
	if err != nil || c.Value == "" {
		httpx.WriteError(w, httpx.ErrUnauthorized("no session"))
		return
	}
	sess, ok, err := h.cfg.Sessions.Lookup(r.Context(), c.Value, h.cfg.now())
	if err != nil {
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}
	if !ok {
		clearAuthCookies(w, h.cfg.Secure)
		httpx.WriteError(w, httpx.ErrUnauthorized("invalid or expired session"))
		return
	}
	httpx.JSON(w, http.StatusOK, sessionUser{User: publicUser(Identity{
		UserID: sess.UserID, Email: sess.Email, DisplayName: sess.DisplayName, Roles: sess.Roles,
	})})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if h.cfg.BypassActor != nil {
		clearAuthCookies(w, h.cfg.Secure)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	c, err := r.Cookie(cookieSession)
	if err != nil || c.Value == "" {
		httpx.WriteError(w, httpx.ErrUnauthorized("no session"))
		return
	}
	now := h.cfg.now()
	sess, ok, err := h.cfg.Sessions.Lookup(r.Context(), c.Value, now)
	if err != nil {
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}
	if !ok {
		clearAuthCookies(w, h.cfg.Secure)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	actorCtx := reqctx.WithActor(r.Context(), reqctx.Actor{UserID: sess.UserID, Type: "user", Label: labelFor(sess), Roles: sess.Roles})
	// The id of the session this logout actually revoked — this device's, and only
	// this device's. It is what the socket hook is told below.
	var revokedID string
	if err := appdb.WithTx(actorCtx, h.db, func(tx *sql.Tx) error {
		_, sessionID, _, err := h.cfg.Sessions.RevokeByToken(actorCtx, tx, c.Value, now)
		if err != nil {
			return err
		}
		revokedID = sessionID
		_, err = h.sink.Record(actorCtx, tx, audit.Event{
			Module: audit.ModulePlatform, Action: "logout",
			Summary: fmt.Sprintf("Odhlášení uživatele %s", sess.Email),
		})
		return err
	}); err != nil {
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}
	// Announced only after the transaction committed, and only for THIS session:
	// the member's other devices hold their own sessions, which this logout did
	// not touch and whose sockets must stay up.
	h.cfg.sessionRevoked(revokedID)
	clearAuthCookies(w, h.cfg.Secure)
	w.WriteHeader(http.StatusNoContent)
}

func writeLoginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBadCredentials):
		httpx.WriteError(w, httpx.ErrUnauthorized("neplatné přihlašovací údaje"))
	case errors.Is(err, ErrDisabled):
		httpx.WriteError(w, httpx.ErrForbidden("účet je zablokován nebo nemá přístup"))
	case errors.Is(err, ErrMFARequired):
		httpx.WriteError(w, &httpx.APIError{Status: http.StatusConflict, Code: "mfa_required", Detail: "dokončete přihlášení na auth.tilcer.cz"})
	default: // ErrUnreachable / anything else
		httpx.WriteError(w, &httpx.APIError{Status: http.StatusBadGateway, Code: "auth_unreachable", Detail: "ověřovací služba je nedostupná"})
	}
}

func publicUser(id Identity) userPublic {
	var dn *string
	if id.DisplayName != "" {
		d := id.DisplayName
		dn = &d
	}
	roles := id.Roles
	if roles == nil {
		roles = []string{}
	}
	return userPublic{ID: id.UserID, Email: id.Email, DisplayName: dn, Roles: roles}
}

func labelForIdentity(id Identity) string {
	if id.DisplayName != "" {
		return id.DisplayName
	}
	return id.Email
}

// normalizeEmail canonicalizes the client-supplied email for rate-limit keying so
// case/whitespace variants share one counter.
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// clientIP mirrors httpx.clientIP for rate-limit keying (X-Forwarded-For aware).
func clientIP(r *http.Request) string {
	if info, ok := reqctx.RequestFrom(r.Context()); ok && info.IP != "" {
		return info.IP
	}
	return r.RemoteAddr
}
