package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// httpAuthenticator talks to the shared auth service BE→BE as the `home` service
// client, authenticating with the X-Service-Secret header.
//
//	!!! ASSUMED CONTRACT — confirm against the ws-tilcer-auth Mode B docs. The
//	    memory'd auth contract covered only Mode A (/introspect, /token/refresh);
//	    /internal/login and /internal/token/mint are specified by home's PRD (§3,
//	    D23) and implemented here behind the Authenticator interface so only this
//	    file changes once the wire shape is confirmed. !!!
//
//	POST {base}/internal/login
//	  headers: X-Service-Secret: <secret>
//	  body:    {"email": "...", "password": "...", "site": "home"}
//	  200 -> {"user_id"|"sub", "email", "display_name", "roles": [...]}
//	  401 -> bad credentials
//	  403 -> disabled / unverified / no access to site
//	  409 -> MFA required (home does not handle)
//	  5xx -> unreachable
//
//	POST {base}/internal/token/mint
//	  headers: X-Service-Secret: <secret>
//	  body:    {"user_id": "...", "site": "home"}
//	  200 -> {"user_id"|"sub", "email", "display_name", "roles": [...]}
//	  403/404/409 -> user closed (disabled/deleted/unverified)
//	  5xx -> unreachable (transient)
type httpAuthenticator struct {
	baseURL string
	secret  string
	site    string
	client  *http.Client
}

// NewHTTPAuthenticator returns an Authenticator backed by the auth service.
func NewHTTPAuthenticator(baseURL, serviceSecret, site string) Authenticator {
	return &httpAuthenticator{
		baseURL: baseURL,
		secret:  serviceSecret,
		site:    site,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

type identityResponse struct {
	UserID      string   `json:"user_id"`
	Sub         string   `json:"sub"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
	Error       string   `json:"error"`
}

func (r identityResponse) identity() Identity {
	id := r.UserID
	if id == "" {
		id = r.Sub
	}
	return Identity{UserID: id, Email: r.Email, DisplayName: r.DisplayName, Roles: r.Roles}
}

func (h *httpAuthenticator) Login(ctx context.Context, email, password string) (Identity, error) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password, "site": h.site})
	resp, err := h.post(ctx, "/internal/login", body)
	if err != nil {
		return Identity{}, ErrUnreachable
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		var ir identityResponse
		if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
			return Identity{}, ErrUnreachable
		}
		return ir.identity(), nil
	case resp.StatusCode == http.StatusUnauthorized:
		return Identity{}, ErrBadCredentials
	case resp.StatusCode == http.StatusForbidden:
		return Identity{}, ErrDisabled
	case resp.StatusCode == http.StatusConflict:
		return Identity{}, ErrMFARequired
	default:
		return Identity{}, ErrUnreachable
	}
}

func (h *httpAuthenticator) Mint(ctx context.Context, userID string) (Identity, error) {
	body, _ := json.Marshal(map[string]string{"user_id": userID, "site": h.site})
	resp, err := h.post(ctx, "/internal/token/mint", body)
	if err != nil {
		return Identity{}, ErrUnreachable
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		var ir identityResponse
		if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
			return Identity{}, ErrUnreachable
		}
		id := ir.identity()
		if id.UserID == "" {
			id.UserID = userID
		}
		return id, nil
	case resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusNotFound,
		resp.StatusCode == http.StatusConflict:
		return Identity{}, ErrUserClosed
	default:
		return Identity{}, ErrUnreachable
	}
}

func (h *httpAuthenticator) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Secret", h.secret)
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth %s: %w", path, err)
	}
	return resp, nil
}
