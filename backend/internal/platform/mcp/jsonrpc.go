package mcp

import (
	"bytes"
	"encoding/json"
)

// JSON-RPC 2.0 over Streamable HTTP (D277/D278).
//
// ⚠ HAND-ROLLED, WITH THE COST STATED RATHER THAN GLOSSED (D278). Home has
// thirteen direct dependencies, all mainstream and pinned, and already hand-rolls
// a websocket hub over coder/websocket. Streamable HTTP is one POST handler and
// an envelope; the official Go SDK is pre-1.0 and moves faster than a household
// app should. The price accepted with that: PROTOCOL-VERSION DRIFT IS OURS TO
// TRACK, and `initialize` is the one place it surfaces.

// protocolVersion is the single MCP revision this server speaks.
//
// ⚠ WHEN THIS CHANGES, CHECK: whether batching is still removed (it was removed
// in 2025-06-18 and rejectBatch below depends on that); whether
// `MCP-Protocol-Version` is still the negotiation header; and whether
// tools/resources/prompts still carry the same result shapes. It is deliberately
// alone here, with nothing else to read past.
const protocolVersion = "2025-06-18"

// protocolVersionHeader is echoed on every response.
const protocolVersionHeader = "MCP-Protocol-Version"

// rpcRequest is one inbound call. `id` is kept as raw JSON because JSON-RPC
// allows a string, a number or null, and echoing back exactly what arrived is
// cheaper and more correct than modelling it.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// isNotification reports whether this is a fire-and-forget call. A notification
// (`notifications/initialized` is the one every client sends) carries no id and
// MUST NOT be answered with a response object.
func (r rpcRequest) isNotification() bool {
	return len(r.ID) == 0 || bytes.Equal(bytes.TrimSpace(r.ID), []byte("null"))
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// newResult wraps one successful answer.
//
// ⚠ A NIL RESULT BECOMES AN EMPTY OBJECT, because `result` is tagged omitempty
// and a response carrying NEITHER `result` nor `error` is not a JSON-RPC
// response at all. `notifications/initialized` is the case that reaches this:
// it is a notification, so it is normally answered with 202 and no body — but a
// client that sends it WITH an id takes the ordinary path, and answering that
// with an empty envelope fails a strict client against a working server.
func newResult(id json.RawMessage, result any) rpcResponse {
	if result == nil {
		result = map[string]any{}
	}
	return rpcResponse{JSONRPC: "2.0", ID: nullID(id), Result: result}
}

func newError(id json.RawMessage, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: nullID(id), Error: &rpcError{Code: code, Message: msg}}
}

// nullID keeps the response's `id` present-and-null when the request had none,
// which is what JSON-RPC requires of an error raised before the id could be read.
func nullID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

// isBatch reports whether the body is a top-level JSON array.
//
// ⚠ A BATCH IS REFUSED RATHER THAN PARTIALLY HONOURED. The 2025-06-18 revision
// removed batching, home does not implement it, and silently processing the first
// element of an array is worse than refusing: the client believes five calls ran.
func isBatch(body []byte) bool {
	for _, b := range body {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '[':
			return true
		default:
			return false
		}
	}
	return false
}
