// Package pgtest hands a test its own Postgres database, and skips the test rather than failing
// it when there is no server to make one on.
package pgtest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"weave/config"

	"github.com/jackc/pgx/v5"
)

const (
	namePrefix = "weave_test_"

	// nameTimeLayout is spelled out rather than left as a count of seconds so that a pid,
	// which is what a worktree still on the old build is putting here, cannot be read as a
	// time and swept out from under a run that is very much alive.
	nameTimeLayout = "20060102t150405"

	// maxAge is deliberately far longer than the ten minutes Go allows a test binary, since a
	// sweep only saves disk and a run under a debugger or a raised timeout still wants its own.
	maxAge = 24 * time.Hour
)

var sweepOnce sync.Once

// URL creates an empty database and drops it when the test ends, leaving the store to
// migrate it so the tests exercise the same path a real start does.
func URL(t *testing.T) string {
	t.Helper()

	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("failed to load config for the test database: %v", err)
	}
	baseURL := cfg.Connection.DatabaseURL

	ctx := t.Context()

	admin, err := pgx.Connect(ctx, replaceDatabase(baseURL, "postgres"))
	if err != nil {
		t.Fatalf("failed to reach postgres; is it running? try 'mise run docker:up': %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	sweepOnce.Do(func() { sweep(ctx, t, admin) })

	name := fmt.Sprintf("%s%s_%016x", namePrefix, time.Now().UTC().Format(nameTimeLayout), rand.Uint64())

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("failed to create test database %q: %v", name, err)
	}

	t.Cleanup(func() {
		dropCtx := context.Background()

		conn, err := pgx.Connect(dropCtx, replaceDatabase(baseURL, "postgres"))
		if err != nil {
			t.Logf("failed to reach postgres to drop %q: %v", name, err)
			return
		}
		defer func() { _ = conn.Close(dropCtx) }()

		if err := drop(dropCtx, conn, name); err != nil {
			t.Logf("failed to drop test database %q: %v", name, err)
		}
	})

	return replaceDatabase(baseURL, name)
}

// sweep drops the test databases left behind by runs that were killed before reaching their
// cleanup, logging what it cannot drop rather than failing a test over housekeeping.
func sweep(ctx context.Context, t *testing.T, conn *pgx.Conn) {
	t.Helper()

	rows, err := conn.Query(ctx, "SELECT datname FROM pg_database WHERE starts_with(datname, $1)", namePrefix)
	if err != nil {
		t.Logf("failed to list test databases to sweep: %v", err)
		return
	}

	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Logf("failed to read the test databases to sweep: %v", err)
		return
	}

	for _, name := range leaked(names, time.Now()) {
		if err := drop(ctx, conn, name); err != nil {
			t.Logf("failed to drop leaked test database %q: %v", name, err)
			continue
		}

		t.Logf("dropped leaked test database %q", name)
	}
}

// leaked picks out the databases old enough that no live run can still hold one, age being the
// only signal safe to sweep on when several worktrees point their suites at one server: a pid
// looks alive the moment the OS hands it to something else, and a database has no connections
// at all in the gap between its creation and the run reaching for it.
func leaked(names []string, now time.Time) []string {
	var stale []string

	for _, name := range names {
		created, ok := createdAt(name)
		if !ok {
			continue
		}

		if now.Sub(created) > maxAge {
			stale = append(stale, name)
		}
	}

	return stale
}

// createdAt reads back the creation time a name carries, leaving a name it cannot read alone
// rather than guessing at one and risking a database some other run is using.
func createdAt(name string) (time.Time, bool) {
	rest, ok := strings.CutPrefix(name, namePrefix)
	if !ok {
		return time.Time{}, false
	}

	stamp, _, ok := strings.Cut(rest, "_")
	if !ok {
		return time.Time{}, false
	}

	parsed, err := time.ParseInLocation(nameTimeLayout, stamp, time.UTC)
	if err != nil {
		return time.Time{}, false
	}

	return parsed, true
}

func drop(ctx context.Context, conn *pgx.Conn, name string) error {
	_, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")

	return err
}

func replaceDatabase(url, name string) string {
	base, query, hasQuery := strings.Cut(url, "?")

	slash := strings.LastIndex(base, "/")
	if slash < 0 {
		return url
	}
	base = base[:slash+1] + name

	if hasQuery {
		return base + "?" + query
	}

	return base
}
