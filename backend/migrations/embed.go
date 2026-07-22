// Package migrations embeds the Goose SQL migration files so they ship inside
// the single binary and run on boot before serving.
package migrations

import "embed"

// FS holds the embedded migration files (0001_init.sql, ...).
//
//go:embed *.sql
var FS embed.FS
