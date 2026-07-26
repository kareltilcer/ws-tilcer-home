package db

import "embed"

// MigrationsFS holds the platform core's Goose migrations (the Mode B session
// store). The registry merges it into the one boot-time migration sequence under
// the "platform" source, in the fixed order logging → platform → todo → events →
// dashboard (PRD §5, D25).
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS
