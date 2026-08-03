package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Vacuum marks the space a delete freed as reusable and refreshes the planner's statistics.
// It hands nothing back to the filesystem, so a table emptied on a cadence plateaus rather
// than shrinking.
//
// Postgres refuses VACUUM inside a transaction, and no statement takes an identifier as a
// parameter, which is why the table names are quoted in rather than bound.
func Vacuum(ctx context.Context, pool *pgxpool.Pool, tables ...string) error {
	for _, table := range tables {
		if _, err := pool.Exec(ctx, "VACUUM (ANALYZE) "+pgx.Identifier{table}.Sanitize()); err != nil {
			return fmt.Errorf("failed to vacuum %s: %w", table, err)
		}
	}

	return nil
}

// TotalRelationSize is what the named tables occupy in bytes, indexes and out-of-line values
// included. A name no table holds contributes nothing rather than failing.
func TotalRelationSize(ctx context.Context, pool *pgxpool.Pool, tables ...string) (int64, error) {
	const query = `
		SELECT COALESCE(SUM(PG_TOTAL_RELATION_SIZE(c.oid)), 0)::bigint
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = CURRENT_SCHEMA() AND c.relname = ANY($1)`

	var total int64
	if err := pool.QueryRow(ctx, query, tables).Scan(&total); err != nil {
		return 0, fmt.Errorf("failed to measure %d tables: %w", len(tables), err)
	}

	return total, nil
}
