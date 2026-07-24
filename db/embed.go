// Package db embeds the Atlas migrations and applies them at runtime, so the
// binary sets up its own SQLite schema without needing the atlas CLI installed.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies any migrations not yet recorded, in filename order. Atlas
// quotes identifiers with backticks, which SQLite accepts, so the raw files run
// as-is. Each is recorded in schema_migrations so it runs once.
func Migrate(conn *sql.DB) error {
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("failed to create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations: %w", err)
	}

	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			versions = append(versions, entry.Name())
		}
	}
	sort.Strings(versions)

	for _, version := range versions {
		var applied int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return fmt.Errorf("failed to check migration %q: %w", version, err)
		}
		if applied > 0 {
			continue
		}

		sqlText, err := migrationsFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("failed to read migration %q: %w", version, err)
		}

		if _, err := conn.Exec(string(sqlText)); err != nil {
			return fmt.Errorf("failed to apply migration %q: %w", version, err)
		}

		if _, err := conn.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			return fmt.Errorf("failed to record migration %q: %w", version, err)
		}
	}

	return nil
}
