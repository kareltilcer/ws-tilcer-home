package mcp

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
)

// The two kinds of failure, and why a model treats them differently (D303).
//
//	A PROTOCOL failure — a JSON-RPC error object, no result:
//	  unknown method, malformed params, unknown tool, unparseable JSON.
//	  The model cannot fix these by trying again with different arguments.
//
//	A TOOL failure — a normal result with isError: true:
//	  validation refused it, the caller may not see it, the thing is not there.
//	  The model reads the text and adjusts.
//
// ⚠ Getting these backwards is not cosmetic: a model RETRIES a protocol error and
// READS a tool error, so a validation refusal shaped as a protocol error becomes a
// loop and an unknown tool shaped as a result becomes a model politely trying
// again with the same name.

// ErrResourceNotFound is what a provider returns for a URI it does not address,
// may not serve, or cannot find. ⚠ ONE error for all three: see notFoundResult.
var ErrResourceNotFound = errors.New("mcp: resource not found")

// UnknownToolError is the protocol-level refusal for a name nothing publishes.
//
// ⚠ THE CRITERION FOR AN ABSENT VERB IS THIS ERROR, NOT A POLITE REFUSAL (D295).
// `home_notes_delete` must come back as *unknown tool* at the protocol level,
// because the absence is the mechanism — a tool that exists and says "I won't" is
// a tool the model keeps proposing.
func UnknownToolError(name string) error {
	return &ProtocolError{Code: CodeInvalidParams, Message: fmt.Sprintf("unknown tool %q", name)}
}

// ProtocolError is a JSON-RPC error object on its way out.
type ProtocolError struct {
	Code    int
	Message string
}

func (e *ProtocolError) Error() string { return e.Message }

// JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// notFoundText is the ONE refusal string, so the two paths that must be
// indistinguishable cannot drift by so much as a trailing space.
//
// ⚠ LEAK ROW 9. On an ownership- or membership-scoped surface a 403 and a 404
// must be INDISTINGUISHABLE. 404-never-403 is a property of the ANSWER, not of
// the status line — a tool that answers "forbidden" leaks exactly what "not
// found" hides, and it does so about the one thing v9 and v10 each spent a whole
// version protecting: whether a private item or a conversation exists at all.
const notFoundText = "Nenalezeno."

// internalText is what an unexpected failure says. ⚠ NO DETAIL: an agent retries
// a 500 and gives up on a 422 (D311), so an internal error must read as
// retryable — and the detail belongs in the Log, under a request id, where a
// person can read it.
const internalText = "Došlo k chybě, zkuste to prosím znovu."

// NotFoundResult is the refusal every ownership- and membership-scoped surface
// returns. It is a FUNCTION returning a value, and the test compares the two
// paths byte for byte, because a refusal that differs by a trailing space is
// still an oracle.
func NotFoundResult() Result { return Result{Text: notFoundText, IsError: true} }

// InternalResult is the refusal for a failure nobody predicted.
func InternalResult() Result { return Result{Text: internalText, IsError: true} }

// toResult maps a provider's error onto what the client sees.
//
// It returns (Result, nil) for anything the model should READ, and
// (Result{}, err) for anything it should treat as a protocol failure — which is
// only ever a *ProtocolError, deliberately: everything else that can go wrong
// inside a tool is something the model can act on.
func toResult(err error) (Result, error) {
	if err == nil {
		return Result{}, nil
	}
	var pe *ProtocolError
	if errors.As(err, &pe) {
		return Result{}, pe
	}
	if errors.Is(err, ErrResourceNotFound) {
		return NotFoundResult(), nil
	}
	var ae *httpx.APIError
	if errors.As(err, &ae) {
		switch ae.Status {
		case http.StatusForbidden, http.StatusNotFound:
			// ⚠ BOTH, THROUGH ONE FUNCTION. A 403 on these surfaces is the module
			// telling the truth to a browser that has a session and a URL bar; over
			// MCP it would be telling an agent that the id it guessed is real.
			return NotFoundResult(), nil
		case http.StatusUnauthorized:
			// Reached only if a service re-checks and finds no actor. The caller IS
			// authenticated — the bearer resolved — so this is a bug, not a login
			// prompt, and it must not read like one.
			return InternalResult(), nil
		case http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusNotImplemented:
			return InternalResult(), nil
		default:
			// 400, 409, 413, 415, 422 — the caller can fix these by sending
			// something else, so the Czech detail goes back verbatim.
			return Result{Text: ae.Detail, IsError: true}, nil
		}
	}
	// An untyped error is an internal failure. It is NEVER echoed: a wrapped
	// sql.ErrNoRows or a driver message is both useless to a model and a
	// description of the database's shape.
	return InternalResult(), nil
}
