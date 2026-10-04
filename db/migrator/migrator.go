// Package migrator applies the Atlas migrations and trigger units db embeds, so a deployment can
// bring its own schema up to date by running the binary rather than by having
// the atlas CLI on the box.
//
// It does this by acquiring an advisory lock (so that two migrations cannot run migrations at
// the same time) and running them each, in sequential order, against the database.
//
// The triggers are embedded alongside them as one versioned file per table, which a pass
// reapplies whenever the file's version moves past the one the database recorded.
package migrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// errSchemaBehind is returned when the database is missing migrations or triggers this build carries.
var errSchemaBehind = errors.New("the database is missing migrations this build carries; run komrad migrate as the database owner")

// Migrate applies any migrations not yet recorded, in filename order, each in its own
// transaction alongside the row recording it, and then any trigger unit the database is behind on.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	src, err := openSources()
	if err != nil {
		return err
	}

	return migrate(ctx, pool, src)
}

// migrate applies the migrations and trigger units in src.
func migrate(ctx context.Context, pool *pgxpool.Pool, src *sources) error {
	contents, err := src.list()
	if err != nil {
		return err
	}

	pending, err := isPending(ctx, pool, contents)
	if err != nil {
		return err
	}
	if !pending {
		return nil
	}

	return apply(ctx, pool, src, contents)
}

// Pending lists the embedded migrations and trigger files that have not run, in the order they
// would run in, and writes nothing.
func Pending(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	src, err := openSources()
	if err != nil {
		return nil, err
	}

	all, err := src.list()
	if err != nil {
		return nil, err
	}

	plan, err := getPlan(ctx, pool, all)
	if err != nil {
		return nil, err
	}

	return plan.names(), nil
}

// CheckSchema refuses a database missing any migration or trigger version this build carries, and accepts one holding newer ones.
func CheckSchema(ctx context.Context, pool *pgxpool.Pool) error {
	src, err := openSources()
	if err != nil {
		return err
	}

	return checkSchema(ctx, pool, src)
}

func checkSchema(ctx context.Context, pool *pgxpool.Pool, src *sources) error {
	all, err := src.list()
	if err != nil {
		return err
	}

	plan, err := getPlan(ctx, pool, all)
	if err != nil {
		return err
	}
	if !plan.empty() {
		return fmt.Errorf("%w: %s", errSchemaBehind, strings.Join(plan.names(), ", "))
	}

	return nil
}
