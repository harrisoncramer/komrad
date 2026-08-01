package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantReadOnlyRole creates the login the MCP query tools connect as and gives it SELECT
// on the application's tables and nothing else.
//
// It is deliberately not a migration. A role is a cluster object while the migration
// runner is per-database, so the row recording it in schema_migrations would describe an
// effect that did not happen in that database, and every throwaway test database would
// replay it. Creating a role also needs CREATEROLE, and migrations run on the way into
// every command including the status line's, which would make an ordinary read require a
// privilege the binary otherwise never uses. So this is its own step, run once by whoever
// provisions the database.
//
// The grant covers the tables that exist and the default privileges cover the ones later
// migrations add, since a default privilege only binds tables created after it is set.
// Both are needed. Running this again is free, which is what lets docker:up call it on
// every start and catch up a database whose schema has moved since.
func GrantReadOnlyRole(ctx context.Context, pool *pgxpool.Pool, name, password string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin granting the read-only role: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The name and password reach the statement as settings rather than as interpolated
	// text. CREATE ROLE takes no bound parameter, so the alternative would be building the
	// statement by hand around a value that comes from a connection string.
	if _, err := tx.Exec(ctx, `SELECT set_config('weave.readonly_role', $1, true)`, name); err != nil {
		return fmt.Errorf("failed to set the read-only role name: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('weave.readonly_password', $1, true)`, password); err != nil {
		return fmt.Errorf("failed to set the read-only role password: %w", err)
	}

	if _, err := tx.Exec(ctx, readOnlyRoleSQL); err != nil {
		return fmt.Errorf("failed to grant the read-only role: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit the read-only role: %w", err)
	}

	return nil
}
