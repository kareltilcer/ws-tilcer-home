// Package seed carries the ONE-OFF historic data migrated from the retiring
// `fin` service (PRD D91, §V6-12): fifteen months, 2025-06 … 2026-08, generated
// from fin's live GET /months with ids and timestamps preserved.
//
// It is a SEPARATE migration source from the finance module's schema, and that
// separation is the whole point. bootstrap.MigrationSources() — which
// testsupport.NewDB migrates with — stays schema-only; only
// MigrationSourcesWithSeed(), called from cmd/home, adds this one. Fold it into
// the schema source and every module test in the repo would run against fifteen
// months of real household finances, and any test that counts rows would be
// written against production data.
//
// The default is the safe one on purpose: a future caller who has never heard of
// this package gets a schema and no household's money.
package seed

import "embed"

// MigrationsFS holds the seed migration (block 09900, applied after the finance
// schema at 09001). It is INSERT OR IGNORE against ux_finance_months_month, so
// applying it to a database that already holds the months is a no-op rather than
// a duplicate-key failure — which is what makes it safe on every boot.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS
