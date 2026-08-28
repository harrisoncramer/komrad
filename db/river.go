package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// RiverSchema is the Postgres schema River keeps its own tables in, separate from the
// application's so that Atlas and River each manage their own migration ledger.
const RiverSchema = "river"

// MigrateRiver creates River's own tables without anyone installing the River CLI, kept
// apart from Migrate since only the process running the queue needs them.
func MigrateRiver(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %q", RiverSchema)); err != nil {
		return fmt.Errorf("failed to create the river schema: %w", err)
	}

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Schema: RiverSchema})
	if err != nil {
		return fmt.Errorf("failed to build the river migrator: %w", err)
	}

	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("failed to migrate river: %w", err)
	}

	return nil
}
