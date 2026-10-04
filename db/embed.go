// Package db embeds the Atlas migrations and trigger units, and holds the helpers every pool
// shares.
package db

import "embed"

// Migrations holds the Atlas migrations, one file per version.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Triggers holds the trigger units, one versioned file per table.
//
//go:embed triggers/*.sql
var Triggers embed.FS
