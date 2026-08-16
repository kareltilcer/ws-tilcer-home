// Package registry is the core of the compile-time modular monolith (PRD §10
// D25/D28, HANDOFF §3). It defines the contracts every feature module registers
// through — routes, migrations, audit actions, and dashboard widget providers —
// and the composition helpers the core uses to build the router, assemble the
// one Goose migration sequence, and expose the widget catalog.
//
// registry lives in platform/ and is imported BY modules; it must never import a
// module. Cross-module data flows only through the WidgetProvider contract — the
// dashboard host reaches feature data through Data(...), never a module's tables.
package registry

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
)

// Module is what every feature module implements to plug into the core. One
// binary, one deploy; modules are compiled in (no runtime plugins, D25).
type Module interface {
	// Name is the English code identifier: "logging" | "todo" | "events" | "dashboard".
	Name() string
	// RegisterRoutes mounts the module's routes on the authenticated /api router.
	// The module applies its own role gates (RequireWrite / RequireAdmin).
	RegisterRoutes(r chi.Router)
	// Migrations returns this module's Goose *.sql files (embedded, under a
	// "migrations/" prefix). May be empty for a module that owns no tables.
	Migrations() fs.FS
	// AuditActions lists the dotted action verbs the module emits, for the log
	// browser's filter catalog. May be empty.
	AuditActions() []string
	// Widgets returns the dashboard widget providers the module contributes. May
	// be empty.
	Widgets() []WidgetProvider
}

// User is the requesting identity passed to a widget provider so it can scope
// its payload to the caller's permissions. It is a decoupled view of the session
// actor — providers depend on registry only, not on the auth internals.
type User struct {
	ID    string
	Email string
	Roles []string
}

// IsAdmin reports whether the user carries the admin role (or the "*" superuser).
func (u User) IsAdmin() bool {
	for _, r := range u.Roles {
		if r == "admin" || r == "*" {
			return true
		}
	}
	return false
}

// WidgetProvider is the single interface through which a module exposes a
// dashboard widget, and the ONLY way feature data reaches the dashboard host
// (PRD §10 D28, FR-M2).
type WidgetProvider interface {
	Key() string         // stable, e.g. "todo.pravedelam"
	Title() string       // Czech display title
	Module() string      // owning module
	Description() string // short Czech description for the catalog ("" if none)
	DefaultSize() string // "narrow" | "wide"
	AdminOnly() bool     // whether only admins may add it
	// Data returns the widget's user-scoped payload. The host calls this; it
	// never queries the owning module's tables.
	Data(ctx context.Context, u User) (any, error)
}

// ---- Widget catalog ----

// CatalogEntry is one row of GET /api/dashboard/catalog (matches openapi
// WidgetCatalogEntry).
type CatalogEntry struct {
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Module      string  `json:"module"`
	Description *string `json:"description"`
	DefaultSize string  `json:"default_size"`
	AdminOnly   bool    `json:"admin_only"`
}

// Catalog is the assembled set of widget providers, keyed by stable key. Built
// by the core from every module's Widgets(); consumed by the dashboard host.
type Catalog struct {
	providers []WidgetProvider
	byKey     map[string]WidgetProvider
}

// NewCatalog builds a catalog from providers, preserving their order and
// rejecting duplicate keys.
func NewCatalog(providers []WidgetProvider) (*Catalog, error) {
	c := &Catalog{byKey: make(map[string]WidgetProvider, len(providers))}
	for _, p := range providers {
		if _, dup := c.byKey[p.Key()]; dup {
			return nil, fmt.Errorf("registry: duplicate widget key %q", p.Key())
		}
		c.byKey[p.Key()] = p
		c.providers = append(c.providers, p)
	}
	return c, nil
}

// Provider looks up a provider by key.
func (c *Catalog) Provider(key string) (WidgetProvider, bool) {
	p, ok := c.byKey[key]
	return p, ok
}

// All returns every registered provider in registration order.
func (c *Catalog) All() []WidgetProvider { return c.providers }

// Entries returns the catalog rows a user may add: all non-admin widgets, plus
// admin-only ones when isAdmin (FR-D1).
func (c *Catalog) Entries(isAdmin bool) []CatalogEntry {
	out := make([]CatalogEntry, 0, len(c.providers))
	for _, p := range c.providers {
		if p.AdminOnly() && !isAdmin {
			continue
		}
		var desc *string
		if d := p.Description(); d != "" {
			desc = &d
		}
		out = append(out, CatalogEntry{
			Key:         p.Key(),
			Title:       p.Title(),
			Module:      p.Module(),
			Description: desc,
			DefaultSize: p.DefaultSize(),
			AdminOnly:   p.AdminOnly(),
		})
	}
	return out
}

// Available reports whether key is a provider the user may use (exists and, if
// admin-only, the user is admin).
func (c *Catalog) Available(key string, isAdmin bool) bool {
	p, ok := c.byKey[key]
	if !ok {
		return false
	}
	return !p.AdminOnly() || isAdmin
}

// ---- Migration assembly ----

// MigrationSource is one contributor of Goose *.sql files: a module (or the
// platform core). Files are embedded under a "migrations/" prefix and carry a
// globally-unique version-number prefix so the merged sequence is deterministic
// and stable across releases (adding a migration never renumbers another's).
type MigrationSource struct {
	Name string
	FS   fs.FS
}

// MergeMigrations flattens every source's migrations/*.sql into a single FS whose
// files goose applies in one ascending sequence. Filenames must be globally
// unique (the version-block convention guarantees this); a collision is a bug and
// fails fast rather than silently dropping a migration.
func MergeMigrations(sources []MigrationSource) (fs.FS, error) {
	merged := fstest.MapFS{}
	for _, src := range sources {
		files, err := fs.Glob(src.FS, "migrations/*.sql")
		if err != nil {
			return nil, fmt.Errorf("registry: glob %s migrations: %w", src.Name, err)
		}
		for _, f := range files {
			name := path.Base(f)
			if _, dup := merged[name]; dup {
				return nil, fmt.Errorf("registry: duplicate migration filename %q (source %s)", name, src.Name)
			}
			b, err := fs.ReadFile(src.FS, f)
			if err != nil {
				return nil, fmt.Errorf("registry: read %s: %w", f, err)
			}
			merged[name] = &fstest.MapFile{Data: b}
		}
	}
	if len(merged) == 0 {
		return nil, fmt.Errorf("registry: no migrations found across %d sources", len(sources))
	}
	return merged, nil
}

// MountAll registers every module's routes on the /api router in the given order.
func MountAll(api chi.Router, modules []Module) {
	for _, m := range modules {
		m.RegisterRoutes(api)
	}
}

// CollectWidgets returns every provider contributed across modules, in module
// order.
func CollectWidgets(modules []Module) []WidgetProvider {
	var out []WidgetProvider
	for _, m := range modules {
		out = append(out, m.Widgets()...)
	}
	return out
}

// ---- Audit action catalog ----

// Action is one audit action key a module can emit, qualified by its module.
// Modules declare bare verbs ("card.move"); the module column qualifies them
// ("todo" + "card.move"), which is how the log browser and the admin trigger
// composer present them.
type Action struct {
	Key    string `json:"key"`    // bare verb, matched against audit_events.action
	Module string `json:"module"` // owning module
	Label  string `json:"label"`  // human Czech phrase, filled by the caller
}

// Qualified is the display form, "module.key".
func (a Action) Qualified() string { return a.Module + "." + a.Key }

// CollectActions returns every audit action the modules declare, in module
// order. This is the vocabulary a v5 trigger rule binds to (FR-ADM2/ADM4) — the
// first consumer of AuditActions(), which until now only documented itself.
//
// Actions emitted by platform/ (login, logout, push.*) belong to no module and
// are merged in by the caller from audit.PlatformActions.
func CollectActions(modules []Module) []Action {
	var out []Action
	for _, m := range modules {
		for _, key := range m.AuditActions() {
			out = append(out, Action{Key: key, Module: m.Name()})
		}
	}
	return out
}
