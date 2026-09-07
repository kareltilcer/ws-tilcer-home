package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
)

// PRD §V11-4 FR-M11 — the leak table, one test per row, each written from the
// attacker's side (HANDOFF-13 §12).
//
// ⚠ v9's equivalent table went from eighteen rows to twenty-three under review
// and the build still found two more nobody had listed. Read this file's length
// as a floor, not as a ceiling.

// Row 1 — /mcp accepts the session cookie ⇒ any website drives the tool surface
// cross-origin with no CSRF.
func TestMCPRejectsCookieOnly(t *testing.T) {
	h := newHarness(t)

	rr := h.rpcWith(func(r *http.Request) {
		// A perfectly good session cookie, and no bearer at all.
		r.AddCookie(&http.Cookie{Name: "session", Value: "a-real-looking-session-token"})
	}, "tools/list", nil)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("a cookie-only request got %d, want 401 — /mcp must not read the session cookie at all (D280).\n%s",
			rr.Code, rr.Body.String())
	}
	// ⚠ AND IT MUST NOT HAVE ANSWERED AS THE COOKIE'S OWNER. A 401 whose body
	// carried a tool list would be the leak wearing a status code.
	if strings.Contains(rr.Body.String(), "home_") {
		t.Fatalf("the refusal body names tools: %s", rr.Body.String())
	}
}

// Row 2 — an unmatched /mcp path falls through to the SPA, returning HTML with a
// 200.
//
// ⚠ ASSERT ON THE CONTENT-TYPE, NOT ON THE STATUS. The failure this guards
// against looks like a working server from the client's side: `index.html` with a
// 200 and a client reporting a protocol error.
func TestMCPPathNeverServesSPA(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{"/mcp/nonsense", "/mcp/", "/mcpx"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			h.handler.ServeHTTP(rr, req)

			ct := rr.Header().Get("Content-Type")
			if strings.Contains(ct, "text/html") {
				t.Fatalf("%s served HTML (%s, %d) — /mcp must join /ws in the router's SPA exclusion (D282)",
					path, ct, rr.Code)
			}
			if !strings.Contains(ct, "application/json") {
				t.Fatalf("%s served %q, want application/json", path, ct)
			}
		})
	}

	// The control: an ordinary SPA route still gets the SPA, or this test would
	// pass against a router that serves nothing at all.
	req := httptest.NewRequest(http.MethodGet, "/poznamky", nil)
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("the SPA fallback itself is broken (%s) — the assertions above prove nothing",
			rr.Header().Get("Content-Type"))
	}
}

// Row 3 — DNS rebinding from a local page.
func TestMCPOriginAllowlist(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	t.Run("a foreign origin is refused", func(t *testing.T) {
		rr := h.rpcWith(func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+secret)
			r.Header.Set("Origin", "https://evil.example")
		}, "tools/list", nil)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("got %d, want 403 for a foreign Origin (D281)", rr.Code)
		}
	})

	t.Run("no origin at all succeeds on a valid bearer", func(t *testing.T) {
		// ⚠ A CLI HAS NO ORIGIN, and refusing it would make the whole surface
		// unusable from the one client v11 is for.
		rr := h.rpc(secret, "tools/list", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200 for an absent Origin with a valid bearer", rr.Code)
		}
	})

	t.Run("no origin and no bearer is still 401", func(t *testing.T) {
		rr := h.rpc("", "tools/list", nil)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401 — the absent-Origin allowance is not an auth bypass", rr.Code)
		}
	})

	t.Run("the allowed origin succeeds", func(t *testing.T) {
		rr := h.rpcWith(func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+secret)
			r.Header.Set("Origin", "https://home.tilcer.cz")
		}, "tools/list", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200 for the allowlisted origin", rr.Code)
		}
	})
}

// Row 4 — a revoked token works while a cached lookup lives.
func TestRevokedTokenFailsNextCall(t *testing.T) {
	h := newHarness(t)
	secret, tok := h.mintToken(memberA, "Claude", nil, time.Time{})

	if rr := h.rpc(secret, "tools/list", nil); rr.Code != http.StatusOK {
		t.Fatalf("the first call got %d, want 200", rr.Code)
	}
	if err := h.tokens.RevokeByID(context.Background(), tok.ID, time.Now()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// ⚠ NO RESTART, NO CACHE EXPIRY, NO SLEEP. There is no positive cache anywhere
	// in the lookup (D287) — that is the whole reason resolution is one indexed
	// seek per call.
	if rr := h.rpc(secret, "tools/list", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("the call after a revoke got %d, want 401", rr.Code)
	}
}

// An expired token is refused, and — leak row 4's twin — is refused the SAME way
// a revoked one is.
func TestExpiredTokenIsRefusedIndistinguishably(t *testing.T) {
	h := newHarness(t)
	expired, _ := h.mintToken(memberA, "Old", nil, time.Now().Add(-time.Hour))
	live, tok := h.mintToken(memberA, "New", nil, time.Time{})
	if err := h.tokens.RevokeByID(context.Background(), tok.ID, time.Now()); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	expiredRR := h.rpc(expired, "tools/list", nil)
	revokedRR := h.rpc(live, "tools/list", nil)
	unknownRR := h.rpc("hmcp_nope", "tools/list", nil)

	for name, rr := range map[string]*httptest.ResponseRecorder{
		"expired": expiredRR, "revoked": revokedRR, "unknown": unknownRR,
	} {
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s token got %d, want 401", name, rr.Code)
		}
	}
	// ⚠ BYTE-IDENTICAL. Which of the three it was is exactly what somebody holding
	// a guessed or stale credential would like to learn.
	if expiredRR.Body.String() != revokedRR.Body.String() || revokedRR.Body.String() != unknownRR.Body.String() {
		t.Fatalf("the three refusals differ:\nexpired=%s\nrevoked=%s\nunknown=%s",
			expiredRR.Body.String(), revokedRR.Body.String(), unknownRR.Body.String())
	}
}

// Row 6 — a token grants more than its owner has.
func TestTokenCannotExceedOwner(t *testing.T) {
	h := newHarness(t)
	// A member whose live identity says `reader`, and a token belonging to them.
	h.seedSession("user-r", "Reader", "reader")
	secret, _ := h.mintToken("user-r", "Claude", nil, time.Time{})

	t.Run("every write tool is refused", func(t *testing.T) {
		for _, tool := range []string{
			"home_todo_card_create", "home_events_create", "home_notes_create",
			"home_todo_card_update", "home_notes_update",
		} {
			_, result := h.call(secret, tool, map[string]any{"title": "x", "column_id": "c", "starts_on": "2026-01-01"})
			if result == nil || !isError(result) {
				t.Fatalf("%s was NOT refused for a reader's token: %#v", tool, result)
			}
		}
	})

	t.Run("read tools still work", func(t *testing.T) {
		_, result := h.call(secret, "home_whoami", nil)
		if result == nil || isError(result) {
			t.Fatalf("home_whoami was refused for a reader's token: %#v", result)
		}
		if !strings.Contains(resultText(t, result), "reader") {
			t.Fatalf("whoami does not report the reader role: %s", resultText(t, result))
		}
	})

	t.Run("the modules allowlist narrows and cannot widen", func(t *testing.T) {
		narrow, _ := h.mintToken(memberA, "Garden only", []string{"garden"}, time.Time{})
		rr := h.rpc(narrow, "tools/list", nil)
		body := rr.Body.String()
		// The core seven are never narrowed — a token that cannot say who it is is a
		// token nobody can debug — but no module tool may appear.
		if !strings.Contains(body, "home_whoami") {
			t.Fatalf("the core tools were narrowed away: %s", body)
		}
		for _, tool := range []string{"home_todo_boards", "home_notes_tree", "home_events_upcoming"} {
			if strings.Contains(body, tool) {
				t.Fatalf("%s is offered to a token scoped to garden alone", tool)
			}
		}
		// ⚠ AND CALLING ONE ANYWAY IS AN UNKNOWN TOOL, not a polite refusal: a token
		// "for the garden" should not be told which other rooms exist.
		rr, result := h.call(narrow, "home_todo_boards", nil)
		if result != nil {
			t.Fatalf("a narrowed-away tool returned a result rather than a protocol error: %#v", result)
		}
		if env := decodeEnvelope(t, rr); env.Error == nil || !strings.Contains(env.Error.Message, "unknown tool") {
			t.Fatalf("want an unknown-tool protocol error, got %#v", env.Error)
		}
	})
}

// Row 7 — home_activity returns raw audit_events including another member's
// private-item summaries and ids.
//
// ⚠ WRITTEN AGAINST A SECOND MEMBER'S PRIVATE ITEM, not the caller's. That is the
// acceptance criterion's own wording, and it is the half a fixture the caller
// owns cannot test: redaction that never fires looks exactly like redaction that
// works.
// ⚠ THE CALLER IS AN ADMIN, because home_activity is admin-only — see
// TestActivityHonoursTheHTTPRoleGate. Redaction is the SECOND rule on this tool
// and this is where it earns its place: an admin is entitled to the Log and is
// still not entitled to another member's private items.
func TestActivityRedactsOtherMembersPrivateItems(t *testing.T) {
	h := newHarness(t)
	// Member B writes a private note through their own service call, so the audit
	// event carries B's ownership marker.
	noteID := h.createPrivateNoteAs(memberB, "Tajný deník", "obsah")
	// And member A — the admin doing the reading — writes one of their own, which
	// is the control below.
	h.createPrivateNoteAs(memberA, "Můj vlastní deník", "obsah")

	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	_, result := h.call(secret, "home_activity", map[string]any{
		"since": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"limit": 100,
	})
	text := resultText(t, result)

	if strings.Contains(text, "Tajný deník") {
		t.Fatalf("member A's activity digest names member B's private note:\n%s", text)
	}
	if strings.Contains(text, noteID) {
		t.Fatalf("member A's activity digest carries member B's private note id:\n%s", text)
	}
	if !strings.Contains(text, "Soukromá poznámka — podrobnosti skryty") {
		t.Fatalf("the redaction phrase is missing — was the event written at all?\n%s", text)
	}

	// The control, in the SAME call: the caller's own private note is named in
	// full, or this test would pass against a digest that hides everything from
	// everybody — redaction that never fires looks exactly like redaction that
	// works.
	if !strings.Contains(text, "Můj vlastní deník") {
		t.Fatalf("the caller cannot see their OWN private note in the digest — the"+
			" redaction is firing on everything:\n%s", text)
	}
}

// home_activity reads the audit spine, and /api/logs/** has been behind
// httpx.RequireAdmin since D5 — so the token must be refused it exactly as its
// owner's browser is (PRD §V11-3, "roles gate exactly as they do over HTTP").
//
// ⚠ THIS IS LEAK ROW 6 IN ITS FOURTH SHAPE. Row 6 is usually read as "a reader
// must be refused every WRITE", and read-only was taken as meaning ungated: the
// digest is read-only and it is still an admin surface, so a token that could
// call it out-ranked its owner by the whole of the household's change history.
func TestActivityHonoursTheHTTPRoleGate(t *testing.T) {
	h := newHarness(t)
	h.createSharedNoteAs(memberA, "Rozpočet domácnosti", "tajné")

	for _, tc := range []struct {
		role  string
		user  string
		admin bool
	}{
		{role: "reader", user: "user-r"},
		{role: "editor", user: "user-e"},
		{role: "admin", user: "user-adm", admin: true},
	} {
		t.Run(tc.role, func(t *testing.T) {
			h.seedSession(tc.user, "Kdo "+tc.role, tc.role)
			secret, _ := h.mintToken(tc.user, "Claude", nil, time.Time{})

			_, result := h.call(secret, "home_activity", map[string]any{
				"since": time.Now().Add(-time.Hour).Format(time.RFC3339),
			})
			switch {
			case tc.admin && isError(result):
				t.Fatalf("an admin's token was refused the digest: %s", resultText(t, result))
			case !tc.admin && !isError(result):
				t.Fatalf("a %s's token read the audit spine that /api/logs refuses"+
					" them with a 403:\n%s", tc.role, resultText(t, result))
			}

			// ⚠ AND IT IS NOT OFFERED IN THE FIRST PLACE. A tool the caller will be
			// refused is a tool the model should not be proposing.
			body := h.rpc(secret, "tools/list", nil).Body.String()
			if listed := strings.Contains(body, "home_activity"); listed != tc.admin {
				t.Fatalf("tools/list offers home_activity to a %s: %t (want %t)",
					tc.role, listed, tc.admin)
			}
		})
	}
}

// Row 8 — home_search returns another member's private note.
func TestSearchIsViewerScoped(t *testing.T) {
	h := newHarness(t)
	h.createPrivateNoteAs(memberB, "Zmrzlina", "jahodová")

	aSecret, _ := h.mintToken(memberA, "Claude A", nil, time.Time{})
	bSecret, _ := h.mintToken(memberB, "Claude B", nil, time.Time{})

	_, aResult := h.call(aSecret, "home_search", map[string]any{"query": "Zmrzlina"})
	if titles := searchTitles(t, aResult); containsString(titles, "Zmrzlina") {
		t.Fatalf("member A found member B's private note: %v", titles)
	}

	// The control, and it is not optional: without it this test also passes
	// against a search that returns nothing to anybody.
	_, bResult := h.call(bSecret, "home_search", map[string]any{"query": "Zmrzlina"})
	if titles := searchTitles(t, bResult); !containsString(titles, "Zmrzlina") {
		t.Fatalf("the OWNER cannot find their own private note — the scoping is too wide"+
			" the other way: %v", titles)
	}

	// And a SHARED note is found by both, so the scoping narrows the private root
	// alone rather than the whole search.
	h.createSharedNoteAs(memberA, "Nákup", "mléko")
	_, shared := h.call(bSecret, "home_search", map[string]any{"query": "Nákup"})
	if titles := searchTitles(t, shared); !containsString(titles, "Nákup") {
		t.Fatalf("member B cannot find a SHARED note: %v", titles)
	}
}

// Row 9 — 404-never-403 breaks in the error mapping.
//
// ⚠ COMPARE THE STRUCTS, NOT THE SENTIMENT. A refusal that differs by a trailing
// space is still an oracle.
func TestRefusalIsIndistinguishable(t *testing.T) {
	h := newHarness(t)
	foreign := h.createPrivateNoteAs(memberB, "Cizí", "obsah")
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	_, foreignResult := h.call(secret, "home_notes_tree", map[string]any{"note": foreign})
	_, unknownResult := h.call(secret, "home_notes_tree", map[string]any{"note": "0192f000-0000-7000-8000-000000000000"})

	foreignJSON, _ := json.Marshal(foreignResult)
	unknownJSON, _ := json.Marshal(unknownResult)
	if string(foreignJSON) != string(unknownJSON) {
		t.Fatalf("reading another member's private note is distinguishable from reading a"+
			" note that does not exist:\nforeign=%s\nunknown=%s", foreignJSON, unknownJSON)
	}
	if !isError(foreignResult) {
		t.Fatalf("the refusal is not an error result: %s", foreignJSON)
	}
	if strings.Contains(strings.ToLower(resultText(t, foreignResult)), "forbid") ||
		strings.Contains(resultText(t, foreignResult), "opráv") {
		t.Fatalf("the refusal says forbidden, which leaks what not-found hides: %s", resultText(t, foreignResult))
	}

	// The same, through home_get, because it is a second door to the same read.
	_, getForeign := h.call(secret, "home_get", map[string]any{"kind": "notes.note", "id": foreign})
	_, getUnknown := h.call(secret, "home_get", map[string]any{"kind": "notes.note", "id": "nope"})
	gf, _ := json.Marshal(getForeign)
	gu, _ := json.Marshal(getUnknown)
	if string(gf) != string(gu) {
		t.Fatalf("home_get distinguishes a foreign private note from an unknown id:\n%s\n%s", gf, gu)
	}
}

// Row 10 — a resource URI treated as a capability.
func TestResourceURIIsNotACapability(t *testing.T) {
	h := newHarness(t)
	h.createPrivateNoteAs(memberB, "Deník", "obsah")

	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	// The URI member B would see. Member A obtained it out of band.
	borrowed := h.rpc(secret, "resources/read", map[string]any{"uri": "home://notes/soukrome/denik"})
	missing := h.rpc(secret, "resources/read", map[string]any{"uri": "home://notes/soukrome/neexistuje"})

	if borrowed.Body.String() != missing.Body.String() {
		t.Fatalf("a borrowed private URI is distinguishable from one that does not exist:\n%s\n%s",
			borrowed.Body.String(), missing.Body.String())
	}
	if !strings.Contains(borrowed.Body.String(), "Nenalezeno") {
		t.Fatalf("the borrowed URI was READ: %s", borrowed.Body.String())
	}
}

// Row 11 — resources/list enumerates what the caller may not read.
func TestResourceListIsViewerScoped(t *testing.T) {
	h := newHarness(t)
	h.createPrivateNoteAs(memberB, "Deník", "obsah")
	h.createSharedNoteAs(memberA, "Recepty", "guláš")

	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	rr := h.rpc(secret, "resources/list", nil)
	body := rr.Body.String()

	if strings.Contains(body, "denik") || strings.Contains(body, "Deník") {
		t.Fatalf("member A's resource listing enumerates member B's private note:\n%s", body)
	}
	if !strings.Contains(body, "recepty") {
		t.Fatalf("the shared note is missing from the listing, so the scoping proves nothing:\n%s", body)
	}
}

// Row 13 — tool descriptions leak household structure to an unauthenticated
// caller.
func TestUnauthenticatedInitializeLeaksNothing(t *testing.T) {
	h := newHarness(t)

	rr := h.rpc("", "initialize", map[string]any{"protocolVersion": "2025-06-18"})
	if rr.Code != http.StatusOK {
		t.Fatalf("unauthenticated initialize got %d, want 200 (D312)", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "home_") {
		t.Fatalf("initialize names tools to an unauthenticated caller:\n%s", body)
	}
	for _, module := range []string{"todo", "notes", "events", "poznámk"} {
		if strings.Contains(strings.ToLower(body), module) {
			t.Fatalf("initialize names module %q to an unauthenticated caller:\n%s", module, body)
		}
	}
	if !strings.Contains(body, "capabilities") {
		t.Fatalf("initialize returned no capabilities at all: %s", body)
	}

	// And every other method is refused.
	for _, method := range []string{"tools/list", "tools/call", "resources/list", "prompts/list"} {
		if rr := h.rpc("", method, nil); rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s without a bearer got %d, want 401", method, rr.Code)
		}
	}
}

// Row 14 — the token is logged, in a request log, an error, or a crash report.
//
// ⚠ THE ASSERTION IS OVER EVERY LOG LINE THE REQUEST PRODUCES, because the crash
// board is fed by the slog handler: whatever reaches a logger reaches status.
func TestNoTokenInLogs(t *testing.T) {
	h, logs := newHarnessCapturingLogs(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	// A successful call, a refused one, and a malformed one — three shapes of line.
	h.rpc(secret, "tools/list", nil)
	h.call(secret, "home_todo_card_create", map[string]any{"title": ""})
	h.rpcWith(func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }, "nope/nope", nil)

	written := logs.String()
	if strings.Contains(written, secret) {
		t.Fatalf("the raw bearer appears in the logs:\n%s", written)
	}
	if strings.Contains(written, "Bearer ") {
		t.Fatalf("an Authorization header appears in the logs:\n%s", written)
	}
	if strings.Contains(written, auth.HashMCPSecret(secret)) {
		t.Fatalf("the token HASH appears in the logs, which is an offline-crackable"+
			" artefact of a credential:\n%s", written)
	}
}

// Row 15 — a tool result large enough to be a denial-of-service against the
// model's context.
func TestResultCapEnforcedByHost(t *testing.T) {
	// A cap small enough that an ordinary note exceeds it.
	h := newHarness(t, withConfig(func(c *mcp.Config) { c.MaxResultBytes = 200 }))
	h.createSharedNoteAs(memberA, "Dlouhá", strings.Repeat("obsah ", 400))

	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	rr := h.rpc(secret, "resources/read", map[string]any{"uri": "home://notes/dlouha"})
	body := rr.Body.String()

	if !strings.Contains(body, "Zkráceno") {
		t.Fatalf("an oversized read was not marked as truncated — silent truncation is"+
			" worse than none, because a model reasons about half a document as though"+
			" it were the whole one:\n%s", body)
	}
	if len(body) > 4000 {
		t.Fatalf("the cap did not apply: %d bytes of body", len(body))
	}
}
