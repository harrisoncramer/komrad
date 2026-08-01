package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// RiverSchema is the Postgres schema River keeps its own tables in. It is separate from
// the application's so Atlas manages one and River manages the other, and neither
// migration ledger has to know about the other's tables.
const RiverSchema = "river"

// MigrateRiver brings River's own tables up to date, so a checkout that has never seen
// River gets them without anyone installing the River CLI.
//
// It is apart from Migrate rather than folded into it because only the process running
// the queue needs these tables. Migrate is on the path every command and every test takes
// through the store, and River's DDL is a dozen tables to pay for on each throwaway
// database.
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
