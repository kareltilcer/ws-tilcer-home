package httpx

import (
	"errors"
	"net/http"
)

// Error is the shared error envelope from openapi.yaml (schema Error).
type Error struct {
	Err    string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

// APIError is a domain error carrying an HTTP status and the client-facing
// envelope. Handlers return these (often via the helpers below) and a single
// place writes them.
type APIError struct {
	Status int
	Code   string
	Detail string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return e.Code + ": " + e.Detail
	}
	return e.Code
}

// Constructors for the common statuses used across the API.

func ErrBadRequest(detail string) *APIError {
	return &APIError{http.StatusBadRequest, "bad_request", detail}
}
func ErrUnauthorized(detail string) *APIError {
	return &APIError{http.StatusUnauthorized, "unauthorized", detail}
}
func ErrForbidden(detail string) *APIError {
	return &APIError{http.StatusForbidden, "forbidden", detail}
}
func ErrNotFound(detail string) *APIError { return &APIError{http.StatusNotFound, "not_found", detail} }
func ErrConflict(detail string) *APIError { return &APIError{http.StatusConflict, "conflict", detail} }

// ErrConflictCode is a 409 carrying its own code, for an endpoint that can refuse for
// more than one reason and whose remedies differ. A client cannot act on a detail
// string, so without a distinct code it has to guess which refusal it hit — and tell
// the user to do the wrong thing about it.
func ErrConflictCode(code, detail string) *APIError {
	return &APIError{http.StatusConflict, code, detail}
}
func ErrUnprocessable(detail string) *APIError {
	return &APIError{http.StatusUnprocessableEntity, "unprocessable", detail}
}

// ErrTooLarge is an upload over the configured size cap (documents, FR-DOC1).
func ErrTooLarge(detail string) *APIError {
	return &APIError{http.StatusRequestEntityTooLarge, "too_large", detail}
}

// ErrUnsupportedMedia is a content type outside the configured allowlist. The
// type is always the SERVER-SNIFFED one, never the client's claim (D48).
func ErrUnsupportedMedia(detail string) *APIError {
	return &APIError{http.StatusUnsupportedMediaType, "unsupported_media_type", detail}
}

// ErrRangeNotSatisfiable rejects a Range header that falls outside the object.
func ErrRangeNotSatisfiable(detail string) *APIError {
	return &APIError{http.StatusRequestedRangeNotSatisfiable, "range_not_satisfiable", detail}
}

// ErrBadGateway reports that an upstream this service depends on (object
// storage, the preview converter) failed. Used so a storage outage never leaves
// a half-committed document (FR-DOC1 step 3).
func ErrBadGateway(detail string) *APIError {
	return &APIError{http.StatusBadGateway, "bad_gateway", detail}
}
func ErrInternal(detail string) *APIError {
	return &APIError{http.StatusInternalServerError, "internal", detail}
}

// ErrNotImplemented reports that a capability exists in the contract but is not
// configured in this deployment (v10, PRD D239).
//
// ⚠ IT IS NOT A 500 AND NOT A 404, and the distinction is the whole point of the
// decision it was added for. Chat's move to Dokumenty needs a `storage.BlobSink`
// wired at composition; with none, the move is UNAVAILABLE and must say so — it
// must never fall back to a delete, because a capability that silently becomes a
// different, destructive capability is worse than one that is plainly absent. The
// client renders no button, so a 501 here means somebody reached the route anyway.
func ErrNotImplemented(detail string) *APIError {
	return &APIError{http.StatusNotImplemented, "not_implemented", detail}
}

// WriteError writes err as the shared Error envelope. Non-APIError values are
// treated as 500s with a generic message (details stay server-side).
func WriteError(w http.ResponseWriter, err error) {
	var ae *APIError
	if errors.As(err, &ae) {
		JSON(w, ae.Status, Error{Err: ae.Code, Detail: ae.Detail})
		return
	}
	JSON(w, http.StatusInternalServerError, Error{Err: "internal"})
}
