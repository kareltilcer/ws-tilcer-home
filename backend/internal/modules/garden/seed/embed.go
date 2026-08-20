// Package seed carries the BUILT-IN compatibility rules — the botanical families
// with their default break years, plus the sourced Czech companion pairs (PRD
// D115, HANDOFF-9 §13).
//
// It is a SEPARATE migration source from the garden module's schema, and that
// separation is the whole point. bootstrap.MigrationSources() — which
// testsupport.NewDB migrates with — stays schema-only; only
// MigrationSourcesWithSeed(), called from cmd/home, adds this one.
//
// Fold it into the schema source and every check fixture in the repo would run
// against a database pre-loaded with fifty-odd companion rules. A C1 test would
// then pass because a SEEDED rule matched rather than the one the test wrote — a
// false green that is very hard to see, and the exact trap v6's finance seed was
// split out to avoid.
//
// The default is the safe one on purpose: a future caller who has never heard of
// this package gets a schema and no opinions about basil.
package seed

import "embed"

// MigrationsFS holds the seed migration (block 10900, applied after the garden
// schema at 10001). It is INSERT OR IGNORE against ux_garden_rules, so applying
// it to a database that already holds the rules is a no-op rather than a
// duplicate-key failure — which is what makes it safe on every boot.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS
