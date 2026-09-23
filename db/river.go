package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// MigrateRiver creates River's own tables in an engine's schema without anyone installing the River CLI.
func MigrateRiver(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Schema: schema})
	if err != nil {
		return fmt.Errorf("failed to build the river migrator: %w", err)
	}

	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("failed to migrate river in %s: %w", schema, err)
	}

	return nil
}
