package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The token management surface — openapi 0.15.0 (PRD §V11-6).
//
// ⚠ THESE ROUTES ARE THE ORDINARY /api SURFACE and are mounted INSIDE the gated
// group: session cookie, CSRF, the lot. They are the half of v11 that OpenAPI can
// describe, and they are how a credential for the OTHER half comes to exist. The
// two never meet — a bearer is refused here exactly as a cookie is refused at
// /mcp.
//
// ⚠ POST /api/mcp/tokens IS THE ONLY RESPONSE IN THIS CONTRACT THAT HAS EVER
// CONTAINED A SECRET, and it contains it once. `mcpTokenWire` — every other read
// — has no secret field at all, so a client cannot log one it never received.

// nameMaxLen matches openapi's McpTokenCreate.name maxLength.
const nameMaxLen = 60

// expiryChoices are the four the contract allows (D285).
//
// ⚠ NOT AN ARBITRARY INTEGER: four choices, so the UI and the contract cannot
// disagree, and so a member picks a decision rather than a date nobody can
// justify.
var expiryChoices = map[int]bool{30: true, 90: true, 365: true}

// MountTokens registers the token routes on the authenticated /api router.
func (h *Host) MountTokens(r chi.Router) {
	r.Route("/mcp/tokens", func(tr chi.Router) {
		tr.Get("/", h.listMyTokens)
		tr.Post("/", h.mintToken)
		tr.Patch("/{id}", h.patchToken)
		tr.Delete("/{id}", h.revokeMyToken)
	})
	r.Route("/admin/mcp/tokens", func(ar chi.Router) {
		// ⚠ THE ADMIN ROUTES ARE SEPARATE ROUTES RATHER THAN A ROLE BRANCH ON THE
		// MEMBER'S, and that asymmetry is the reason: on the member's routes another
		// member's token is a 404, because a 403 would confirm the id exists and turn
		// a guessed id into an oracle over who has connected an assistant. On the
		// admin's, an existing id IS revocable and a non-existent one IS a 404 —
		// an admin is entitled to know these exist. One route with a branch would
		// have to answer both, and would get one of them wrong.
		ar.Use(httpx.RequireAdmin)
		ar.Get("/", h.listAllTokens)
		ar.Delete("/{id}", h.revokeAnyToken)
	})
}

// ---- wire types ----

type mcpTokenWire struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Modules    []string `json:"modules"`
	CreatedAt  string   `json:"created_at"`
	ExpiresAt  *string  `json:"expires_at"`
	LastUsedAt *string  `json:"last_used_at"`
	LastUsedIP *string  `json:"last_used_ip"`
	RevokedAt  *string  `json:"revoked_at"`
}

type mcpTokenCreatedWire struct {
	mcpTokenWire
	Secret string `json:"secret"`
}

type mcpTokenAdminWire struct {
	mcpTokenWire
	UserID      string  `json:"user_id"`
	DisplayName *string `json:"display_name"`
}

func tokenWire(t auth.MCPToken) mcpTokenWire {
	return mcpTokenWire{
		ID:         t.ID,
		Name:       t.Name,
		Prefix:     t.Prefix,
		Modules:    appdb.OrEmpty(t.Modules),
		CreatedAt:  t.CreatedAt.Format(time.RFC3339),
		ExpiresAt:  tsPtr(t.ExpiresAt),
		LastUsedAt: tsPtr(t.LastUsedAt),
		LastUsedIP: strPtr(t.LastUsedIP),
		RevokedAt:  tsPtr(t.RevokedAt),
	}
}

func tsPtr(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---- handlers ----

func (h *Host) listMyTokens(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.ActorID(r.Context())
	toks, err := h.deps.Tokens.ListByUser(r.Context(), userID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]mcpTokenWire, 0, len(toks))
	for _, t := range toks {
		out = append(out, tokenWire(t))
	}
	httpx.JSON(w, http.StatusOK, out)
}

type mintRequest struct {
	Name string `json:"name"`
	// ExpiresInDays is a pointer so an explicit null — "bez omezení" — is
	// distinguishable from an absent field, which is a 422.
	ExpiresInDays *int     `json:"expires_in_days"`
	Modules       []string `json:"modules"`
}

func (h *Host) mintToken(w http.ResponseWriter, r *http.Request) {
	var in mintRequest
	present, err := httpx.DecodePatch(r, &in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if !present["expires_in_days"] {
		httpx.WriteError(w, httpx.ErrUnprocessable("expires_in_days is required (30, 90, 365 or null)"))
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		httpx.WriteError(w, httpx.ErrUnprocessable("Název tokenu nesmí být prázdný."))
		return
	}
	if len([]rune(name)) > nameMaxLen {
		httpx.WriteError(w, httpx.ErrUnprocessable("Název tokenu je příliš dlouhý."))
		return
	}
	if in.ExpiresInDays != nil && !expiryChoices[*in.ExpiresInDays] {
		httpx.WriteError(w, httpx.ErrUnprocessable("Platnost musí být 30, 90, 365 dní nebo bez omezení."))
		return
	}
	mods, err := h.normaliseModules(in.Modules)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	ctx := r.Context()
	userID := reqctx.ActorID(ctx)
	now := h.now()

	// ⚠ COUNTED BEFORE THE INSERT AND NOT INSIDE IT. This is a hygiene bound, not
	// a security boundary (D289) — a list nobody can read is a list nobody revokes
	// from — so a race that admits an eleventh token is not a hole, and taking a
	// write lock to close it would cost the one connection on every mint.
	live, err := h.deps.Tokens.CountLive(ctx, userID, now)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if live >= h.deps.Config.MaxTokensPerUser {
		httpx.WriteError(w, httpx.ErrUnprocessable("Máte maximální počet tokenů. Odvolejte některý starý."))
		return
	}

	secret, hash, prefix, err := auth.NewMCPSecret()
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var expiresAt time.Time
	if in.ExpiresInDays != nil {
		expiresAt = now.UTC().AddDate(0, 0, *in.ExpiresInDays)
	}

	var created auth.MCPToken
	err = appdb.WithTx(ctx, h.deps.DB, func(tx *sql.Tx) error {
		t, err := h.deps.Tokens.Create(ctx, tx, userID, name, hash, prefix, mods, expiresAt, now)
		if err != nil {
			return err
		}
		created = t
		// ⚠ THE EVENT CARRIES THE NAME AND THE PREFIX, NEVER THE SECRET AND NEVER
		// THE HASH. The prefix is what makes a Log row matchable to a config file by
		// eye; the hash in a log would be an offline-crackable artefact of a
		// credential, in the one place the household reads regularly.
		return h.sink().Record(ctx, tx, audit.Event{
			Action:     "mcp.token.create",
			EntityType: "mcp_token",
			EntityID:   t.ID,
			Summary:    "Vytvořen token pro asistenta „" + name + "“",
			Meta:       map[string]any{"name": name, "prefix": prefix, "modules": mods},
		})
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, mcpTokenCreatedWire{mcpTokenWire: tokenWire(created), Secret: secret})
}

type patchRequest struct {
	Name    *string  `json:"name"`
	Modules []string `json:"modules"`
	// ⚠ THE TWO EXPIRY SPELLINGS ARE DECLARED HERE AND NEVER READ, on purpose.
	// httpx.DecodePatch sets DisallowUnknownFields, so without them a body
	// carrying one is refused as "unknown field" — a 422 that says nothing about
	// what to do instead. Declared, they reach the explicit refusal below, which
	// names the only remedy that exists: mint a new token (D285).
	ExpiresAt     json.RawMessage `json:"expires_at"`
	ExpiresInDays json.RawMessage `json:"expires_in_days"`
}

func (h *Host) patchToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var in patchRequest
	present, err := httpx.DecodePatch(r, &in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	// ⚠ SENDING AN EXPIRY IS A 422, NOT A SILENT NO-OP (D285). A field quietly
	// ignored is how a member comes to believe a token was extended — and the one
	// remedy that actually exists, minting a new one, is a decision they should
	// have to take deliberately.
	if present["expires_at"] || present["expires_in_days"] {
		httpx.WriteError(w, httpx.ErrUnprocessable(
			"Platnost tokenu nelze změnit. Vytvořte nový token."))
		return
	}

	tok, err := h.ownedToken(ctx, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	name := tok.Name
	if present["name"] {
		if in.Name == nil {
			httpx.WriteError(w, httpx.ErrUnprocessable("Název tokenu nesmí být prázdný."))
			return
		}
		name = strings.TrimSpace(*in.Name)
		if name == "" {
			httpx.WriteError(w, httpx.ErrUnprocessable("Název tokenu nesmí být prázdný."))
			return
		}
		if len([]rune(name)) > nameMaxLen {
			httpx.WriteError(w, httpx.ErrUnprocessable("Název tokenu je příliš dlouhý."))
			return
		}
	}
	mods := tok.Modules
	if present["modules"] {
		mods, err = h.normaliseModules(in.Modules)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
	}

	var changes []audit.Change
	if name != tok.Name {
		changes = append(changes, audit.Change{Field: "name", Old: &tok.Name, New: &name})
	}
	if strings.Join(mods, ",") != strings.Join(tok.Modules, ",") {
		oldM, newM := strings.Join(tok.Modules, ","), strings.Join(mods, ",")
		changes = append(changes, audit.Change{Field: "modules", Old: &oldM, New: &newM})
	}

	err = appdb.WithTx(ctx, h.deps.DB, func(tx *sql.Tx) error {
		if err := h.deps.Tokens.Update(ctx, tx, id, name, mods); err != nil {
			return err
		}
		return h.sink().Record(ctx, tx, audit.Event{
			Action:     "mcp.token.update",
			EntityType: "mcp_token",
			EntityID:   id,
			Summary:    "Upraven token pro asistenta „" + name + "“",
			Changes:    changes,
			Meta:       map[string]any{"prefix": tok.Prefix},
		})
	})
	if err != nil {
		httpx.WriteError(w, mapTokenErr(err))
		return
	}
	updated, err := h.deps.Tokens.Get(ctx, id)
	if err != nil {
		httpx.WriteError(w, mapTokenErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, tokenWire(updated))
}

func (h *Host) revokeMyToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tok, err := h.ownedToken(ctx, chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w, h.revoke(ctx, tok, nil))
}

func (h *Host) listAllTokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	toks, err := h.deps.Tokens.ListAll(ctx)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	names := map[string]string{}
	if h.deps.Names != nil {
		if got, err := h.deps.Names(ctx); err == nil {
			names = got
		} else {
			// A directory failure costs the owner COLUMN, not the page: an admin who
			// cannot see whose token this is can still see that it exists and revoke
			// it, which is the whole of what this screen is for.
			h.deps.Logger.Warn("mcp admin token listing: member directory unavailable", "err", err)
		}
	}
	out := make([]mcpTokenAdminWire, 0, len(toks))
	for _, t := range toks {
		row := mcpTokenAdminWire{mcpTokenWire: tokenWire(t), UserID: t.UserID}
		if n := names[t.UserID]; n != "" {
			row.DisplayName = &n
		}
		out = append(out, row)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Host) revokeAnyToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tok, err := h.deps.Tokens.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, mapTokenErr(err))
		return
	}
	owner := tok.UserID
	httpx.NoContent(w, h.revoke(ctx, tok, &owner))
}

// revoke stamps revoked_at and audits it.
//
// onBehalfOf is non-nil for the admin route, and it is what lets the Log
// distinguish a member revoking their own key from an admin revoking somebody's
// — the same act with a very different meaning.
func (h *Host) revoke(ctx context.Context, tok auth.MCPToken, onBehalfOf *string) error {
	meta := map[string]any{"name": tok.Name, "prefix": tok.Prefix}
	summary := "Odvolán token pro asistenta „" + tok.Name + "“"
	if onBehalfOf != nil {
		meta["on_behalf_of"] = *onBehalfOf
	}
	now := h.now()
	return appdb.WithTx(ctx, h.deps.DB, func(tx *sql.Tx) error {
		if err := h.deps.Tokens.Revoke(ctx, tx, tok.ID, now); err != nil {
			return err
		}
		return h.sink().Record(ctx, tx, audit.Event{
			Action:     "mcp.token.revoke",
			EntityType: "mcp_token",
			EntityID:   tok.ID,
			Summary:    summary,
			Meta:       meta,
		})
	})
}

// ownedToken loads a token the CALLER owns.
//
// ⚠ ANOTHER MEMBER'S TOKEN IS A 404, NEVER A 403 (the v9 D180 rule). A 403
// confirms the id exists, which turns a guessed id into an oracle over who in the
// household has connected an assistant — and the ids are UUIDv7, so they are
// time-ordered and not as unguessable as they look.
func (h *Host) ownedToken(ctx context.Context, id string) (auth.MCPToken, error) {
	tok, err := h.deps.Tokens.Get(ctx, id)
	if err != nil {
		return auth.MCPToken{}, mapTokenErr(err)
	}
	if tok.UserID != reqctx.ActorID(ctx) {
		return auth.MCPToken{}, httpx.ErrNotFound("no such token")
	}
	return tok, nil
}

// normaliseModules validates a module allowlist against the live registry.
//
// ⚠ An unknown module is a 422 rather than being dropped: a token silently scoped
// to nothing looks identical to a broken server from the client's side, and the
// member who typed the typo is the only person who can fix it.
func (h *Host) normaliseModules(mods []string) ([]string, error) {
	if len(mods) == 0 {
		return []string{}, nil
	}
	known := map[string]bool{}
	for _, m := range h.deps.Registry.Modules() {
		known[m] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(mods))
	for _, m := range mods {
		m = strings.TrimSpace(m)
		if !known[m] {
			return nil, httpx.ErrUnprocessable("Neznámý modul: " + m)
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out, nil
}

func (h *Host) sink() audit.ModuleSink {
	return audit.For(h.deps.Sink, audit.ModulePlatform)
}

func mapTokenErr(err error) error {
	if errors.Is(err, auth.ErrTokenNotFound) {
		return httpx.ErrNotFound("no such token")
	}
	return err
}
