package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// httpIntrospector calls the shared auth service's introspection endpoint,
// authenticating as the `home` service client.
//
// Contract confirmed against ws-tilcer-auth (backend/openapi.yaml + internal_be.go):
//   - POST {AUTH_BASE_URL}/introspect
//   - service-client auth via the `X-Service-Secret: <secret>` header (bound to
//     one site; a token for another site returns active:false)
//   - JSON request  {"token": "<jwt>", "site": "home"} (site advisory)
//   - JSON response {"active": bool, "sub", "email", "type", "site",
//     "roles": ["..."], "exp": <unix seconds>}
//   - HTTP 200 with active:false for an invalid/expired/foreign-site token;
//     401 only for a missing/invalid service secret
type httpIntrospector struct {
	baseURL string
	secret  string
	site    string
	client  *http.Client
}

// NewHTTPIntrospector returns an Introspector backed by the auth service.
func NewHTTPIntrospector(baseURL, serviceSecret, site string) Introspector {
	return &httpIntrospector{
		baseURL: baseURL,
		secret:  serviceSecret,
		site:    site,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

type introspectRequest struct {
	Token string `json:"token"`
	Site  string `json:"site"`
}

type introspectResponse struct {
	Active bool     `json:"active"`
	Sub    string   `json:"sub"`
	Email  string   `json:"email"`
	Roles  []string `json:"roles"`
	Exp    int64    `json:"exp"`
}

func (h *httpIntrospector) Introspect(ctx context.Context, token string) (Claims, error) {
	body, err := json.Marshal(introspectRequest{Token: token, Site: h.site})
	if err != nil {
		return Claims{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/introspect", bytes.NewReader(body))
	if err != nil {
		return Claims{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Secret", h.secret)

	resp, err := h.client.Do(req)
	if err != nil {
		return Claims{}, fmt.Errorf("introspect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Claims{}, fmt.Errorf("introspect: auth service returned %d", resp.StatusCode)
	}
	var ir introspectResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		return Claims{}, fmt.Errorf("introspect: decode response: %w", err)
	}
	if !ir.Active {
		return Claims{Active: false}, nil
	}
	exp := time.Time{}
	if ir.Exp > 0 {
		exp = time.Unix(ir.Exp, 0)
	}
	return Claims{
		Active:    true,
		UserID:    ir.Sub,
		Roles:     ir.Roles,
		Label:     ir.Email,
		ExpiresAt: exp,
	}, nil
}
