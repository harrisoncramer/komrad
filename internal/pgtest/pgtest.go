// Package pgtest hands each test its own Postgres database.
//
// The tests run against a real database rather than a fake because most of what
// the store does is SQL. The partial unique index on live names, the keyset page,
// the trigram search, the triggers that stamp updated_at. A double would assert
// that the Go around those works and say nothing about the parts that break.
//
// One database per test rather than one shared with truncation between them, so
// a test that leaves a row behind cannot change what the next one sees, and the
// package is safe to run in parallel with itself.
package pgtest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"weave/config"

	"github.com/jackc/pgx/v5"
)

// created counts the databases this process has made, so their names cannot
// collide within a run. The pid keeps them apart between concurrent runs, which
// is how `go test ./...` behaves, one process per package, all at once.
var created atomic.Int64

// URL creates an empty database and returns a connection string for it, dropping
// it when the test ends. The schema is not applied here. The caller opens the
// store against this url and the store migrates it, so the tests exercise the
// same migration path a real start does.
func URL(t *testing.T) string {
	t.Helper()

	// The test database is created on the server the application would have
	// connected to, read through the same config, so `mise run docker:up` is the only
	// setup a test run needs and a CI runner redirects the whole run by setting
	// DATABASE_URL, the read-only login included, since that one is derived from it.
	// Only the server is borrowed. The database named in the url is never touched.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config for the test database: %v", err)
	}
	baseURL := cfg.DatabaseURL

	name := fmt.Sprintf("weave_test_%d_%d", os.Getpid(), created.Add(1))

	ctx := t.Context()

	admin, err := pgx.Connect(ctx, replaceDatabase(baseURL, "postgres"))
	if err != nil {
		t.Fatalf("failed to reach postgres; is it running? try 'mise run docker:up': %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	// The name is generated above from a pid and a counter rather than from input, so
	// interpolating it is safe. CREATE DATABASE takes no bound parameters anyway.
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", name)); err != nil {
		t.Fatalf("failed to create test database %q: %v", name, err)
	}

	t.Cleanup(func() {
		// A fresh context, since the test's own is already cancelled by the time
		// cleanup runs and the drop still has to reach the server. WITH (FORCE) evicts
		// any connection the test left open.
		dropCtx := context.Background()

		conn, err := pgx.Connect(dropCtx, replaceDatabase(baseURL, "postgres"))
		if err != nil {
			t.Logf("failed to reach postgres to drop %q: %v", name, err)
			return
		}
		defer func() { _ = conn.Close(dropCtx) }()

		if _, err := conn.Exec(dropCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", name)); err != nil {
			t.Logf("failed to drop test database %q: %v", name, err)
		}
	})

	return replaceDatabase(baseURL, name)
}

// replaceDatabase swaps the database name in a connection string, which is how
// the admin connection reaches the server without going through the database it
// is about to create or drop.
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
