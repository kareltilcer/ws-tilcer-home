package auth

// Internal tests (package auth) for the Mode B httpAuthenticator: they exercise
// the unexported JWT verification, so they live alongside the implementation.
// auth returns a signed access token whose claims carry identity + roles; these
// tests prove home reads them and rejects anything it cannot trust.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

const (
	testJWTSecret = "shared-hs256-secret"
	testIssuer    = "https://auth.tilcer.cz"
	testSite      = "home"
)

// mintTestToken forges a site-scoped access token the same way ws-tilcer-auth
// does, so verifyToken sees a realistic input.
func mintTestToken(t *testing.T, secret, iss, aud, sub, email, name string, roles []string, exp time.Time, method jwt.SigningMethod) string {
	t.Helper()
	claims := accessClaims{
		Site:  aud,
		Roles: roles,
		Email: email,
		Name:  name,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    iss,
			Subject:   sub,
			Audience:  jwt.ClaimStrings{aud},
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	}
	tok := jwt.NewWithClaims(method, claims)
	var key any = []byte(secret)
	if method == jwt.SigningMethodNone {
		key = jwt.UnsafeAllowNoneSignatureType
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return s
}

func testAuthr() *httpAuthenticator {
	return &httpAuthenticator{
		baseURL:   testIssuer,
		secret:    "svc",
		jwtSecret: []byte(testJWTSecret),
		site:      testSite,
	}
}

func TestVerifyToken_ValidTokens(t *testing.T) {
	h := testAuthr()
	future := time.Now().Add(10 * time.Minute)

	t.Run("superuser wildcard maps through unchanged", func(t *testing.T) {
		raw := mintTestToken(t, testJWTSecret, testIssuer, testSite, "u-super", "boss@tilcer.cz", "Boss", []string{"*"}, future, jwt.SigningMethodHS256)
		id, err := h.verifyToken(raw)
		if err != nil {
			t.Fatalf("verifyToken(superuser) error: %v", err)
		}
		if id.UserID != "u-super" || id.Email != "boss@tilcer.cz" || id.DisplayName != "Boss" {
			t.Errorf("identity = %+v, want u-super/boss/Boss", id)
		}
		if len(id.Roles) != 1 || id.Roles[0] != "*" {
			t.Errorf("roles = %v, want [*] (this is the bug: superuser must arrive as [\"*\"])", id.Roles)
		}
	})

	t.Run("editor role", func(t *testing.T) {
		raw := mintTestToken(t, testJWTSecret, testIssuer, testSite, "u1", "marie@tilcer.cz", "", []string{"editor"}, future, jwt.SigningMethodHS256)
		id, err := h.verifyToken(raw)
		if err != nil {
			t.Fatalf("verifyToken(editor) error: %v", err)
		}
		if len(id.Roles) != 1 || id.Roles[0] != "editor" || id.DisplayName != "" {
			t.Errorf("identity = %+v, want roles=[editor], empty name", id)
		}
	})
}

func TestVerifyToken_Rejects(t *testing.T) {
	h := testAuthr()
	future := time.Now().Add(10 * time.Minute)

	cases := []struct {
		name string
		raw  func() string
	}{
		{"wrong signing secret", func() string {
			return mintTestToken(t, "not-the-secret", testIssuer, testSite, "u1", "e@x", "N", []string{"admin"}, future, jwt.SigningMethodHS256)
		}},
		{"token for another site (audience)", func() string {
			return mintTestToken(t, testJWTSecret, testIssuer, "blog", "u1", "e@x", "N", []string{"admin"}, future, jwt.SigningMethodHS256)
		}},
		{"expired", func() string {
			return mintTestToken(t, testJWTSecret, testIssuer, testSite, "u1", "e@x", "N", []string{"admin"}, time.Now().Add(-time.Minute), jwt.SigningMethodHS256)
		}},
		{"alg none (alg-confusion)", func() string {
			return mintTestToken(t, "", testIssuer, testSite, "u1", "e@x", "N", []string{"admin"}, future, jwt.SigningMethodNone)
		}},
		{"wrong issuer", func() string {
			return mintTestToken(t, testJWTSecret, "https://evil.example", testSite, "u1", "e@x", "N", []string{"admin"}, future, jwt.SigningMethodHS256)
		}},
		{"not a jwt", func() string { return "garbage.not.jwt" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.verifyToken(tc.raw()); err == nil {
				t.Errorf("verifyToken accepted a token it must reject (%s)", tc.name)
			}
		})
	}
}

// Tolerate a trailing slash difference between AUTH_BASE_URL and the token issuer
// so a benign config-format mismatch doesn't lock every user out.
func TestVerifyToken_IssuerTrailingSlashTolerated(t *testing.T) {
	h := testAuthr()
	h.baseURL = testIssuer + "/"
	raw := mintTestToken(t, testJWTSecret, testIssuer, testSite, "u1", "e@x", "N", []string{"admin"}, time.Now().Add(time.Minute), jwt.SigningMethodHS256)
	if _, err := h.verifyToken(raw); err != nil {
		t.Errorf("trailing-slash issuer should verify, got: %v", err)
	}
}

// End-to-end: Login and Mint decode auth's TokenResponse envelope and read the
// identity/roles from the JWT inside it.
func TestLoginAndMint_ReadRolesFromToken(t *testing.T) {
	future := time.Now().Add(10 * time.Minute)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The token issuer must equal how home addresses auth (its base URL).
		iss := srv.URL
		var sub string
		var roles []string
		switch r.URL.Path {
		case "/internal/login":
			sub, roles = "u1", []string{"*"} // superuser logs in
		case "/internal/token/mint":
			sub, roles = "u1", []string{"admin"}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw := mintTestToken(t, testJWTSecret, iss, testSite, sub, "boss@tilcer.cz", "Boss", roles, future, jwt.SigningMethodHS256)
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: raw, TokenType: "Bearer", ExpiresIn: 900, Site: testSite})
	}))
	defer srv.Close()

	h := &httpAuthenticator{baseURL: srv.URL, secret: "svc", jwtSecret: []byte(testJWTSecret), site: testSite, client: srv.Client()}

	id, err := h.Login(context.Background(), "boss@tilcer.cz", "pw")
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}
	if id.UserID != "u1" || len(id.Roles) != 1 || id.Roles[0] != "*" {
		t.Errorf("Login identity = %+v, want u1 with roles [*]", id)
	}

	mid, err := h.Mint(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Mint error: %v", err)
	}
	if mid.UserID != "u1" || len(mid.Roles) != 1 || mid.Roles[0] != "admin" {
		t.Errorf("Mint identity = %+v, want u1 with roles [admin]", mid)
	}
}

// A TOTP-enabled account answers /internal/login with an MFA challenge (200, no
// token). Home does not handle MFA, so Login must surface ErrMFARequired.
func TestLogin_MFAChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"mfa_required": true, "mfa_token": "xyz"})
	}))
	defer srv.Close()

	h := &httpAuthenticator{baseURL: srv.URL, secret: "svc", jwtSecret: []byte(testJWTSecret), site: testSite, client: srv.Client()}
	if _, err := h.Login(context.Background(), "e@x", "pw"); err != ErrMFARequired {
		t.Errorf("Login MFA challenge err = %v, want ErrMFARequired", err)
	}
}

// A 200 carrying a token home cannot verify (e.g. a secret mismatch) must fail
// closed rather than minting an empty-roles session.
func TestLogin_UnverifiableToken_FailsClosed(t *testing.T) {
	future := time.Now().Add(10 * time.Minute)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		raw := mintTestToken(t, "attacker-secret", testIssuer, testSite, "u1", "e@x", "N", []string{"*"}, future, jwt.SigningMethodHS256)
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: raw, TokenType: "Bearer", Site: testSite})
	}))
	defer srv.Close()

	h := &httpAuthenticator{baseURL: srv.URL, secret: "svc", jwtSecret: []byte(testJWTSecret), site: testSite, client: srv.Client()}
	if _, err := h.Login(context.Background(), "e@x", "pw"); err != ErrUnreachable {
		t.Errorf("unverifiable token err = %v, want ErrUnreachable (fail closed)", err)
	}
}
