package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantReadOnlyRole creates the login the MCP query tools connect as and gives it SELECT on
// the application's tables and nothing else.
func GrantReadOnlyRole(ctx context.Context, pool *pgxpool.Pool, name, password string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin granting the read-only role: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT set_config('weave.readonly_role', $1, true)`, name); err != nil {
		return fmt.Errorf("failed to set the read-only role name: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('weave.readonly_password', $1, true)`, password); err != nil {
		return fmt.Errorf("failed to set the read-only role password: %w", err)
	}

	// Explainer: the SQL leaves the role alone once it exists and only reapplies the grants.
	// Roles are shared by the whole cluster, so altering one from several databases at once
	// fails all but one with "tuple concurrently updated", which is what the test suite does
	// when it gives every test its own database. An advisory lock cannot serialize that, since
	// those are scoped to one database. The cost is that a password is never rotated, so
	// changing one means dropping the role first.
	if _, err := tx.Exec(ctx, readOnlyRoleSQL); err != nil {
		return fmt.Errorf("failed to grant the read-only role: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit the read-only role: %w", err)
	}

	return nil
}
