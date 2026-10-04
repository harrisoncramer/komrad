package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// RiverMigrator creates River's own tables in an engine's schema without anyone installing the River CLI.
type RiverMigrator struct {
	pool *pgxpool.Pool
}

// NewRiverMigrator builds a RiverMigrator that migrates through pool.
func NewRiverMigrator(pool *pgxpool.Pool) *RiverMigrator {
	return &RiverMigrator{pool: pool}
}

// Migrate brings River's tables in schema up to date.
func (r *RiverMigrator) Migrate(ctx context.Context, schema string) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(r.pool), &rivermigrate.Config{Schema: schema})
	if err != nil {
		return fmt.Errorf("failed to build the river migrator: %w", err)
	}

	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("failed to migrate river in %s: %w", schema, err)
	}

	return nil
}
