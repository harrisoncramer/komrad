package doctor

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

// connectTimeout bounds reaching a database. A Postgres that has not answered in this long
// is one the panel would fail to start against anyway, and the point of the check is to say
// so rather than to wait with it.
const connectTimeout = 5 * time.Second

// pgProbe is a database opened for looking at and nothing else.
type pgProbe struct {
	pool *pgxpool.Pool
}

// Pending is the migrations this build ships that the database has not applied. It reads
// the ledger and writes nothing, so a database behind on migrations stays behind.
func (p *pgProbe) Pending(ctx context.Context) ([]string, error) {
	return db.Pending(ctx, p.pool)
}

func (p *pgProbe) Close() { p.pool.Close() }

// connectProbe opens the writable login without letting it write.
//
// This is deliberately not store.Open, which applies the migrations on the way in. Doctor
// reports whether the schema is behind, and a check that fixed the thing it was measuring
// could never report it. The read-only flag rides on the connection, so the guarantee is
// Postgres's rather than this code's.
func connectProbe(ctx context.Context, url string) (Probe, error) {
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

	// NewWithConfig does not dial, so without this a database that is not there comes
	// back as a working handle and fails on the first query instead.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, err
	}

	return &pgProbe{pool: pool}, nil
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
