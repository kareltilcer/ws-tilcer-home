package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
)

// The credential's lifecycle (PRD §V11-4 FR-M3, §V11-6, D284–D289, D316).

// ⚠ THE SECRET APPEARS IN EXACTLY ONE RESPONSE IN THE WHOLE CONTRACT, and this is
// that response.
func TestMintReturnsTheSecretOnceAndStoresOnlyItsHash(t *testing.T) {
	h := newHarness(t)

	rr := h.api(http.MethodPost, "/api/mcp/tokens",
		`{"name":"Claude na notebooku","expires_in_days":30}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("mint got %d: %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID        string  `json:"id"`
		Secret    string  `json:"secret"`
		Prefix    string  `json:"prefix"`
		ExpiresAt *string `json:"expires_at"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(created.Secret, auth.TokenPrefix) {
		t.Fatalf("the secret does not carry the greppable %q prefix: %q", auth.TokenPrefix, created.Secret)
	}
	// ⚠ THE PREFIX IS THE FIRST 8 CHARACTERS OF THE WHOLE STRING — `hmcp_` plus
	// three — so a member can match a row in Nastavení to a line in a config file
	// by eye.
	if created.Prefix != created.Secret[:8] {
		t.Fatalf("prefix %q is not the secret's first 8 characters", created.Prefix)
	}

	// The database holds the hash and NOT the secret. ⚠ A dump must contain no
	// usable credential — that is the property that makes a Litestream replica of
	// this table grant nothing.
	var storedHash string
	if err := h.db.QueryRow(`SELECT token_hash FROM mcp_tokens WHERE id = ?`, created.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if storedHash != auth.HashMCPSecret(created.Secret) {
		t.Fatalf("token_hash is not the SHA-256 of the secret")
	}
	if strings.Contains(storedHash, created.Secret) {
		t.Fatalf("the secret is in the database")
	}

	// And no LIST response ever carries it again.
	list := h.api(http.MethodGet, "/api/mcp/tokens", "")
	if strings.Contains(list.Body.String(), created.Secret) {
		t.Fatalf("the listing returns the secret: %s", list.Body.String())
	}
	if strings.Contains(list.Body.String(), storedHash) {
		t.Fatalf("the listing returns the HASH: %s", list.Body.String())
	}

	// The audit event carries the name and the prefix, never the secret or hash.
	var meta string
	if err := h.db.QueryRow(`
		SELECT COALESCE(meta, '') FROM audit_events
		 WHERE module = 'platform' AND action = 'mcp.token.create'`).Scan(&meta); err != nil {
		t.Fatalf("read the mint audit event: %v", err)
	}
	if !strings.Contains(meta, created.Prefix) || !strings.Contains(meta, "Claude na notebooku") {
		t.Fatalf("the audit event does not identify the token: %s", meta)
	}
	if strings.Contains(meta, created.Secret) || strings.Contains(meta, storedHash) {
		t.Fatalf("the audit event carries the secret or the hash: %s", meta)
	}
}

// ⚠ THE EXPIRY IS WRITTEN ONCE AND NEVER SLIDES (D285), and `last_used_at`
// advances at most once an hour (D286). The resemblance to a session is the trap:
// a session extends on use, and a token that renewed itself on every call would
// be a token that never expires.
func TestExpiryNeverSlidesAndLastUsedIsHourly(t *testing.T) {
	h := newHarness(t)
	expires := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	secret, tok := h.mintToken(memberA, "Claude", nil, expires)

	before := h.tokenRow(tok.ID)
	for range 50 {
		if rr := h.rpc(secret, "ping", nil); rr.Code != http.StatusOK {
			t.Fatalf("ping got %d", rr.Code)
		}
	}
	after := h.tokenRow(tok.ID)

	if before.expiresAt != after.expiresAt {
		t.Fatalf("expires_at moved across 50 calls: %q → %q", before.expiresAt, after.expiresAt)
	}
	if before.lastUsed == after.lastUsed && before.lastUsed != "" {
		t.Fatalf("last_used_at never advanced at all — the column is dead")
	}
	// Fifty calls inside one second must produce ONE write, not fifty: on a pool of
	// ONE connection every read tool would otherwise become a writer.
	stamps := map[string]bool{}
	for range 5 {
		if rr := h.rpc(secret, "ping", nil); rr.Code != http.StatusOK {
			t.Fatalf("ping got %d", rr.Code)
		}
		stamps[h.tokenRow(tok.ID).lastUsed] = true
	}
	if len(stamps) != 1 {
		t.Fatalf("last_used_at advanced %d times inside an hour, want 1 (D286)", len(stamps))
	}
}

// ⚠ PATCH CANNOT CHANGE THE EXPIRY, and sending one is a 422 rather than a silent
// no-op: a field quietly ignored is how a member comes to believe a token was
// extended (D285).
func TestPatchRefusesTheExpiryAndRenamesInPlace(t *testing.T) {
	h := newHarness(t)
	_, tok := h.mintToken(memberA, "Starý název", nil, time.Now().Add(time.Hour))

	for _, body := range []string{
		`{"expires_at":"2099-01-01T00:00:00Z"}`,
		`{"expires_in_days":365}`,
		`{"name":"Nový","expires_in_days":null}`,
	} {
		rr := h.api(http.MethodPatch, "/api/mcp/tokens/"+tok.ID, body)
		if rr.Code != http.StatusUnprocessableEntity {
			t.Fatalf("PATCH %s got %d, want 422", body, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Vytvořte nový token") {
			t.Fatalf("the refusal does not name the remedy that exists: %s", rr.Body.String())
		}
	}

	// A rename works, and the expiry does not move with it.
	before := h.tokenRow(tok.ID)
	rr := h.api(http.MethodPatch, "/api/mcp/tokens/"+tok.ID, `{"name":"Nový název"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("rename got %d: %s", rr.Code, rr.Body.String())
	}
	after := h.tokenRow(tok.ID)
	if after.name != "Nový název" {
		t.Fatalf("the rename did not land: %q", after.name)
	}
	if before.expiresAt != after.expiresAt {
		t.Fatalf("the expiry moved on a rename: %q → %q", before.expiresAt, after.expiresAt)
	}
}

// The expiry choices are four, not an arbitrary integer (D285).
func TestMintValidatesItsInputs(t *testing.T) {
	h := newHarness(t)

	cases := map[string]string{
		"an expiry outside the four choices":  `{"name":"x","expires_in_days":45}`,
		"an absent expiry":                    `{"name":"x"}`,
		"an empty name":                       `{"name":"   ","expires_in_days":30}`,
		"a name past 60 characters":           `{"name":"` + strings.Repeat("a", 61) + `","expires_in_days":30}`,
		"a module outside the contract's set": `{"name":"x","expires_in_days":30,"modules":["kitchen"]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if rr := h.api(http.MethodPost, "/api/mcp/tokens", body); rr.Code != http.StatusUnprocessableEntity {
				t.Fatalf("got %d, want 422: %s", rr.Code, rr.Body.String())
			}
		})
	}

	// ⚠ A MODULE THAT PUBLISHES NOTHING YET IS STILL VALID. v11 ships as three
	// PRs, so between PR 1 and PR 2 six of the contract's nine modules have no
	// provider — refusing them would contradict the served openapi.yaml, and the
	// allowlist NARROWS rather than granting (D288).
	rr := h.api(http.MethodPost, "/api/mcp/tokens", `{"name":"Zahrada","expires_in_days":30,"modules":["garden"]}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("a token scoped to a not-yet-published module got %d: %s", rr.Code, rr.Body.String())
	}

	// null is "bez omezení" and is accepted.
	if rr := h.api(http.MethodPost, "/api/mcp/tokens", `{"name":"Navždy","expires_in_days":null}`); rr.Code != http.StatusCreated {
		t.Fatalf("a never-expiring token got %d: %s", rr.Code, rr.Body.String())
	}
}

// The eleventh LIVE token is refused (D289) — a hygiene bound, not a security one.
func TestLiveTokenCeiling(t *testing.T) {
	h := newHarness(t, withConfig(func(c *mcp.Config) { c.MaxTokensPerUser = 2 }))

	for i := range 2 {
		if rr := h.api(http.MethodPost, "/api/mcp/tokens", `{"name":"t","expires_in_days":30}`); rr.Code != http.StatusCreated {
			t.Fatalf("token %d got %d", i+1, rr.Code)
		}
	}
	rr := h.api(http.MethodPost, "/api/mcp/tokens", `{"name":"t","expires_in_days":30}`)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the token past the ceiling got %d, want 422", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Odvolejte") {
		t.Fatalf("the refusal does not say what to do: %s", rr.Body.String())
	}

	// ⚠ AND A REVOKED TOKEN DOES NOT COUNT, or the ceiling would be a ratchet: a
	// member who revoked ten would never be able to mint again.
	var id string
	if err := h.db.QueryRow(`SELECT id FROM mcp_tokens LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("read a token: %v", err)
	}
	if rr := h.api(http.MethodDelete, "/api/mcp/tokens/"+id, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("revoke got %d", rr.Code)
	}
	if rr := h.api(http.MethodPost, "/api/mcp/tokens", `{"name":"t","expires_in_days":30}`); rr.Code != http.StatusCreated {
		t.Fatalf("minting after a revoke got %d — the ceiling counts dead tokens", rr.Code)
	}
}

// ⚠ DELETE IS A REVOKE AND THE ROW STAYS, because an audit event's `via_token_id`
// must still resolve to a name years later.
func TestRevokeStampsAndKeepsTheRow(t *testing.T) {
	h := newHarness(t)
	_, tok := h.mintToken(memberA, "Claude", nil, time.Time{})

	if rr := h.api(http.MethodDelete, "/api/mcp/tokens/"+tok.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("revoke got %d", rr.Code)
	}
	row := h.tokenRow(tok.ID)
	if row.revokedAt == "" {
		t.Fatalf("revoked_at was not stamped")
	}
	if row.name != "Claude" {
		t.Fatalf("the row was deleted or renamed: %#v", row)
	}
	// Idempotent: revoking twice is not an error, because "is this key dead" has
	// the same answer either way.
	if rr := h.api(http.MethodDelete, "/api/mcp/tokens/"+tok.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("the second revoke got %d", rr.Code)
	}
	if h.tokenRow(tok.ID).revokedAt != row.revokedAt {
		t.Fatalf("the second revoke moved revoked_at")
	}
}

// ⚠ ANOTHER MEMBER'S TOKEN IS A 404, NEVER A 403 (the v9 D180 rule): a 403
// confirms the id exists, which turns a guessed id into an oracle over who has
// connected an assistant.
func TestForeignTokenIsNotFoundOnTheMembersRoutes(t *testing.T) {
	h := newHarness(t)
	_, foreign := h.mintToken(memberB, "Petra's Claude", nil, time.Time{})

	patch := h.api(http.MethodPatch, "/api/mcp/tokens/"+foreign.ID, `{"name":"mine now"}`)
	del := h.api(http.MethodDelete, "/api/mcp/tokens/"+foreign.ID, "")
	unknownPatch := h.api(http.MethodPatch, "/api/mcp/tokens/does-not-exist", `{"name":"x"}`)
	unknownDel := h.api(http.MethodDelete, "/api/mcp/tokens/does-not-exist", "")

	for name, rr := range map[string]int{
		"patch a foreign token":  patch.Code,
		"delete a foreign token": del.Code,
		"patch an unknown id":    unknownPatch.Code,
		"delete an unknown id":   unknownDel.Code,
	} {
		if rr != http.StatusNotFound {
			t.Fatalf("%s got %d, want 404", name, rr)
		}
	}
	if patch.Body.String() != unknownPatch.Body.String() {
		t.Fatalf("a foreign token is distinguishable from an unknown id:\n%s\n%s",
			patch.Body.String(), unknownPatch.Body.String())
	}
	// And it is genuinely still there — the 404 is a refusal, not a delete.
	if _, err := h.tokens.Get(context.Background(), foreign.ID); err != nil {
		t.Fatalf("the foreign token was actually modified: %v", err)
	}
}

// ⚠ AN ADMIN MAY SEE THAT A KEY EXISTS AND TAKE IT AWAY, AND MAY NOT MAKE ONE IN
// SOMEBODY ELSE'S NAME (D316). D240's shape — names and sizes, and no way in —
// applied to credentials.
func TestAdminListingHasNoSecretAndNoMint(t *testing.T) {
	h := newHarness(t)
	_, tok := h.mintToken(memberB, "Petřin asistent", nil, time.Time{})
	secret, _ := h.mintToken(memberA, "Karlův asistent", nil, time.Time{})

	rr := h.api(http.MethodGet, "/api/admin/mcp/tokens", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("admin listing got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Petřin asistent") || !strings.Contains(body, memberB) {
		t.Fatalf("the admin listing does not show another member's token: %s", body)
	}
	if !strings.Contains(body, "Petra") {
		t.Fatalf("the admin listing does not name the owner: %s", body)
	}
	if strings.Contains(body, secret) || strings.Contains(body, auth.HashMCPSecret(secret)) {
		t.Fatalf("the admin listing carries a secret or a hash: %s", body)
	}

	// ⚠ THERE IS NO ADMIN MINT ANYWHERE IN THE CONTRACT.
	if rr := h.api(http.MethodPost, "/api/admin/mcp/tokens", `{"name":"x","expires_in_days":30}`); rr.Code == http.StatusCreated {
		t.Fatalf("an admin minted a token in another member's name")
	}

	// An admin may revoke another member's, and the event says on whose behalf.
	if rr := h.api(http.MethodDelete, "/api/admin/mcp/tokens/"+tok.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("admin revoke got %d: %s", rr.Code, rr.Body.String())
	}
	var meta string
	if err := h.db.QueryRow(`
		SELECT COALESCE(meta, '') FROM audit_events
		 WHERE action = 'mcp.token.revoke' ORDER BY ts DESC LIMIT 1`).Scan(&meta); err != nil {
		t.Fatalf("read the revoke event: %v", err)
	}
	if !strings.Contains(meta, "on_behalf_of") || !strings.Contains(meta, memberB) {
		t.Fatalf("the admin revoke does not record whose token it was: %s", meta)
	}
}

// A non-admin is refused the admin listing.
func TestAdminTokenRoutesAreAdminOnly(t *testing.T) {
	h := newHarnessAs(t, "editor")
	if rr := h.api(http.MethodGet, "/api/admin/mcp/tokens", ""); rr.Code != http.StatusForbidden {
		t.Fatalf("an editor got %d on the admin listing, want 403", rr.Code)
	}
}
