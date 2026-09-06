package mcp

import (
	"encoding/json"
	"strings"
)

// Prompts (v11, PRD §V11-4 FR-M8, D309).
//
// Five, Czech-named and Czech-bodied, English tool calls underneath. They are the
// cheapest thing in v11 and the place household knowledge actually lives —
// `/večeře-z-toho-co-máme` is two garden tools and a sentence about what this
// family eats, and it is worth more than any three tools.
//
// ⚠ THE HANDOFF SAID `garden.storage` + `garden.harvests` AND ITS OWN TOOL TABLE
// PUBLISHES NEITHER AS A READ — garden is six tools (`tasks` · `task_create` ·
// `task_complete` · `plan` · `planting_create` · `harvest_log`): the sklad is not
// there at all, and `harvest_log` WRITES a harvest rather than reading them back.
// Writing the prompt to the prose rather than to the table would have shipped
// precisely the unknown-tool failure described two paragraphs down, so the body
// reaches for `home_garden_plan` and `home_garden_tasks` and says in one visible
// line that the sklad is not readable from here. ⚠ THE LINE IS IN THE BODY rather
// than in this comment on purpose: the model reads the body, and a model that does
// not know a thing is missing invents it.
//
// ⚠ KEPT AS DATA, NOT AS FILES: five short strings do not need a loader.
//
// ⚠ THE SET SHIPPED EMPTY IN PR 1 AND IS FILLED HERE, deliberately. Four of the
// five name tools that did not exist until PR 2 — garden's, finance's,
// documents' — and a prompt telling a model to call a tool the server does not
// publish is worse than no prompt: the model follows it, gets an unknown-tool
// error, and reports the server as broken.
//
// ⚠ EVERY TOOL NAME IN A PROMPT BODY IS ASSERTED AGAINST THE REGISTRY
// (TestPromptsOnlyNameToolsThatExist). A prompt is the one place in v11 where a
// tool name is written as PROSE rather than as code, so nothing but a test
// notices when a tool is renamed — and the failure is silent at exactly the
// moment somebody is relying on the prompt.

// Prompt is one published prompt.
type Prompt struct {
	Name        string // "co-mě-čeká" — Czech, as the member types it
	Title       string
	Description string
	Arguments   []PromptArgument
	// Text is the prompt body handed to the model, in Czech.
	//
	// ⚠ EVERY DECLARED ARGUMENT APPEARS IN HERE AS `{{name}}`, and a test asserts
	// it. An argument published in `prompts/list` and never substituted is the
	// silently-dropped value this version refuses everywhere else — the member
	// types a month, the body never mentions it, and the model closes out the one
	// the body's fallback names instead.
	Text string
}

// PromptArgument is one templated input.
type PromptArgument struct {
	Name        string
	Description string
	Required    bool
}

// prompts returns the published set.
//
// ⚠ THEY ARE CZECH-NAMED AND CZECH-BODIED WITH ENGLISH TOOL CALLS UNDERNEATH
// (D309). The name is what a member types; the body is what the model reads; the
// tools are the English ones the catalog publishes. Mixing the three languages
// in one string is the point rather than an oversight.
//
// ⚠ AND THE HOUSEHOLD KNOWLEDGE IS THE VALUABLE PART. `večeře-z-toho-co-máme` is
// two tool calls and a sentence about what this family actually eats — the tool
// calls are worth little without it, and it lives nowhere else in the system.
func (h *Host) prompts() []Prompt {
	return []Prompt{
		{
			Name:        "co-mě-čeká",
			Title:       "Co mě čeká",
			Description: "Přehled dneška a nejbližších dnů — připomínky, úkoly, práce na zahradě.",
			Text: "Zjisti, co mě dnes čeká.\n\n" +
				"1. Zavolej `home_whoami`, ať víš, kdo jsem a jaké je dnes datum.\n" +
				"2. Zavolej `home_today` — vrátí připomínky, úkoly i zahradní práci na dnešek.\n" +
				"3. Zavolej `home_events_upcoming` na příštích sedm dní.\n" +
				"4. Zavolej `home_chat_conversations` a zmiň jen počet nepřečtených zpráv, " +
				"ne jejich obsah.\n\n" +
				"Odpověz česky, stručně, jako by mi to někdo řekl u snídaně: nejdřív to, co má " +
				"termín dnes, pak zbytek týdne. Když není nic, řekni to jednou větou — nevymýšlej " +
				"úkoly a nenavrhuj, co bych mohl dělat.",
		},
		{
			Name:        "nákup",
			Title:       "Nákupní seznam",
			Description: "Sestaví nákupní seznam z poznámek a úkolů, se zásobami ze zahrady.",
			Text: "Sestav nákupní seznam.\n\n" +
				"1. Zavolej `home_search` s dotazem \"nákup\" — projde poznámky i úkoly.\n" +
				"2. Otevři nalezené poznámky přes `home_notes_tree` s parametrem `note`.\n" +
				"3. Zavolej `home_todo_boards` a projdi karty, které vypadají jako nákup.\n\n" +
				"Slož z toho jeden seznam po kategoriích (pečivo, mléčné, zelenina, drogerie, " +
				"ostatní). Duplicity slouč. Pokud najdeš věc, kterou máme podle zahrady doma, " +
				"napiš to k položce místo toho, abys ji tiše vynechal — rozhodnutí je moje.\n\n" +
				"Nic nikam nezapisuj, dokud tě o to nepožádám.",
		},
		{
			Name:        "večeře-z-toho-co-máme",
			Title:       "Večeře z toho, co máme",
			Description: "Navrhne večeři ze zahradních zásob a poslední sklizně.",
			Text: "Navrhni večeři z toho, co máme doma.\n\n" +
				"1. Zavolej `home_search` s dotazem \"recept\" a projdi, co v poznámkách je.\n" +
				"2. Zavolej `home_garden_plan` a `home_garden_tasks` — co je teď na zahradě " +
				"a co se právě sklízí.\n\n" +
				"O téhle domácnosti platí: vaříme spíš jednoduše a večer, maso párkrát týdně, " +
				"a co je ze zahrady, to se snažíme sníst dřív, než se to zkazí. Navrhni dvě " +
				"varianty, u každé napiš, co k ní chybí koupit.\n\n" +
				"⚠ Zásoby ve skladu (sklad zahrady) přes MCP zatím vidět nejsou — jestli si " +
				"nejsi jistý, co je doma, zeptej se místo hádání.",
		},
		{
			Name:        "měsíční-uzávěrka",
			Title:       "Měsíční uzávěrka",
			Description: "Projde finance a elektřinu za měsíc a řekne, co ještě chybí zapsat.",
			Arguments: []PromptArgument{{
				Name:        "měsíc",
				Description: "Měsíc ve tvaru RRRR-MM. Když ho neuvedeš, vezme se ten minulý.",
			}},
			Text: "Projdi se mnou uzávěrku měsíce.\n\n" +
				"Měsíc, o který jde: {{měsíc}}\n" +
				"(Když je tenhle řádek prázdný, vezmi měsíc předcházející dnešnímu datu.)\n\n" +
				"1. Zavolej `home_whoami` kvůli dnešnímu datu.\n" +
				"2. Zavolej `home_finance_months` na ten měsíc. Když chybí, řekni to — " +
				"nezakládej ho sám.\n" +
				"3. Zavolej `home_electricity_summary` a `home_electricity_readings`.\n\n" +
				"Napiš česky: jestli jsou příjmy za měsíc zapsané, jaký vyšel rozpad, jestli " +
				"je odečet elektroměru čerstvý, a jestli vychází nedoplatek nebo přeplatek. " +
				"Na konci uveď seznam toho, co ještě chybí zapsat — a počkej, až ti řeknu, " +
				"co s tím.",
		},
		{
			Name:        "zahrada-týden",
			Title:       "Zahrada na týden",
			Description: "Co je na zahradě potřeba udělat tento týden a co je po termínu.",
			Text: "Řekni mi, co je tento týden potřeba na zahradě.\n\n" +
				"1. Zavolej `home_whoami` kvůli datu.\n" +
				"2. Zavolej `home_garden_tasks` na příštích sedm dní.\n" +
				"3. Zavolej `home_garden_plan` — ať víš, co kde roste.\n\n" +
				"Seřaď to podle záhonů, ne podle data: chodí se po zahradě, ne po kalendáři. " +
				"Co je po termínu, dej nahoru a označ to. Když je něco hotové, můžu tě " +
				"požádat o `home_garden_task_complete` — sám nic neodškrtávej.",
		},
	}
}

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
	// ⚠ THE ARGUMENTS ARE READ, NOT ACCEPTED AND IGNORED. The protocol's own
	// shape is a flat map of strings, and `měsíční-uzávěrka` is the one prompt
	// that declares one.
	Arguments map[string]string `json:"arguments"`
}

func (h *Host) promptsGet(params json.RawMessage) (any, error) {
	var in promptsGetParams
	if err := json.Unmarshal(params, &in); err != nil || in.Name == "" {
		return nil, &ProtocolError{Code: CodeInvalidParams, Message: "prompts/get requires a name"}
	}
	for _, p := range h.prompts() {
		if p.Name != in.Name {
			continue
		}
		text, perr := renderPrompt(p, in.Arguments)
		if perr != nil {
			return nil, perr
		}
		return map[string]any{
			"description": p.Description,
			"messages": []any{map[string]any{
				"role":    "user",
				"content": map[string]any{"type": "text", "text": text},
			}},
		}, nil
	}
	// A protocol error, not an empty result: the model cannot fix an unknown
	// prompt name by trying different arguments.
	return nil, &ProtocolError{Code: CodeInvalidParams, Message: "unknown prompt " + in.Name}
}

// renderPrompt substitutes the caller's arguments into the body.
//
// ⚠ AN ARGUMENT THAT REACHES NO PLACEHOLDER IS A VALUE SILENTLY DROPPED, and that
// is the one shape this version refuses everywhere else: an unknown module is a
// 422, an unknown `via` is a 422, an expiry on PATCH is a 422 rather than a
// no-op, and `DecodeArgs` is strict for exactly this reason. A prompt argument
// is worse than the rest of them, because there is no error at all — the member
// types a month, the model never sees it, and the answer is about a different
// month that looks entirely plausible.
//
// ⚠ AN UNDECLARED NAME IS REFUSED rather than ignored, on the same argument. A
// client sending `mesic` for `měsíc` gets told; it does not get a close-out of
// last month with no indication that anything was dropped.
func renderPrompt(p Prompt, args map[string]string) (string, *ProtocolError) {
	declared := make(map[string]bool, len(p.Arguments))
	for _, a := range p.Arguments {
		declared[a.Name] = true
	}
	for name := range args {
		if !declared[name] {
			return "", &ProtocolError{
				Code:    CodeInvalidParams,
				Message: "prompt " + p.Name + " has no argument " + name,
			}
		}
	}
	text := p.Text
	for _, a := range p.Arguments {
		value := strings.TrimSpace(args[a.Name])
		if a.Required && value == "" {
			return "", &ProtocolError{
				Code:    CodeInvalidParams,
				Message: "prompt " + p.Name + " requires the argument " + a.Name,
			}
		}
		text = strings.ReplaceAll(text, "{{"+a.Name+"}}", value)
	}
	return text, nil
}
