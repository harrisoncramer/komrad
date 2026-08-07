package doctor

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

// connectTimeout bounds reaching a database; a Postgres that has not answered in this long
// is one the panel would fail to start against anyway.
const connectTimeout = 5 * time.Second

// pingDatabase reports whether the writable login answers.
func pingDatabase(ctx context.Context, url string) error {
	pool, err := openReadOnlyPool(ctx, url)
	if err != nil {
		return err
	}
	pool.Close()

	return nil
}

// readPendingMigrations lists what this build ships that the database has not applied.
func readPendingMigrations(ctx context.Context, url string) ([]string, error) {
	pool, err := openReadOnlyPool(ctx, url)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	return db.Pending(ctx, pool)
}

// openReadOnlyPool opens the writable login without letting it write. This is deliberately
// not store.Open, which applies migrations on the way in: a check that fixed the schema it
// was measuring could never report it behind, so the read-only flag rides on the
// connection, making Postgres the one enforcing it.
func openReadOnlyPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("failed to read the database url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// NewWithConfig does not dial, so without this a database that is not there comes back
	// as a working handle and fails on the first query instead.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, err
	}

	return pool, nil
}

// connectReadOnly opens the least-privilege login the way the service does, so a machine
// that passes this gets the service past the same call rather than one that resembles it.
func connectReadOnly(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	pool, err := store.OpenReadOnly(ctx, url)
	if err != nil {
		return err
	}
	pool.Close()

	return nil
}
