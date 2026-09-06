package mcp

import (
	"encoding/json"
)

// Prompts (v11, PRD §V11-4 FR-M8, D309).
//
// Five, Czech-named and Czech-bodied, English tool calls underneath. They are the
// cheapest thing in v11 and the place household knowledge actually lives —
// `/večeře-z-toho-co-máme` is garden.storage + garden.harvests and a sentence
// about what this family eats, and it is worth more than any three tools.
//
// ⚠ KEPT AS DATA, NOT AS FILES: five short strings do not need a loader.
//
// ⚠ THE SET IS EMPTY IN PR 1 AND FILLED IN PR 3, deliberately. Four of the five
// name tools that do not exist until PR 2 — garden's, finance's, documents' — and
// a prompt telling a model to call `home_garden_storage` against a server that
// publishes no such tool is worse than no prompt: the model follows it, gets an
// unknown-tool error, and reports the server as broken. The machinery ships here
// so `prompts/list` is a real answer rather than a method-not-found.

// Prompt is one published prompt.
type Prompt struct {
	Name        string // "co-mě-čeká" — Czech, as the member types it
	Title       string
	Description string
	Arguments   []PromptArgument
	// Text is the prompt body handed to the model, in Czech.
	Text string
}

// PromptArgument is one templated input.
type PromptArgument struct {
	Name        string
	Description string
	Required    bool
}

// prompts returns the published set.
func (h *Host) prompts() []Prompt { return nil }

func promptsWire(ps []Prompt) []map[string]any {
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		args := make([]map[string]any, 0, len(p.Arguments))
		for _, a := range p.Arguments {
			args = append(args, map[string]any{
				"name":        a.Name,
				"description": a.Description,
				"required":    a.Required,
			})
		}
		out = append(out, map[string]any{
			"name":        p.Name,
			"title":       p.Title,
			"description": p.Description,
			"arguments":   args,
		})
	}
	return out
}

type promptsGetParams struct {
	Name string `json:"name"`
}

func (h *Host) promptsGet(params json.RawMessage) (any, error) {
	var in promptsGetParams
	if err := json.Unmarshal(params, &in); err != nil || in.Name == "" {
		return nil, &ProtocolError{Code: CodeInvalidParams, Message: "prompts/get requires a name"}
	}
	for _, p := range h.prompts() {
		if p.Name == in.Name {
			return map[string]any{
				"description": p.Description,
				"messages": []any{map[string]any{
					"role":    "user",
					"content": map[string]any{"type": "text", "text": p.Text},
				}},
			}, nil
		}
	}
	// A protocol error, not an empty result: the model cannot fix an unknown
	// prompt name by trying different arguments.
	return nil, &ProtocolError{Code: CodeInvalidParams, Message: "unknown prompt " + in.Name}
}
