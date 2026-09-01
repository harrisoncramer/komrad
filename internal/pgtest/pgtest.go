// Package pgtest hands a test its own Postgres database, and skips the test rather than failing
// it when there is no server to make one on.
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

var created atomic.Int64

// URL creates an empty database and drops it when the test ends, leaving the store to
// migrate it so the tests exercise the same path a real start does.
func URL(t *testing.T) string {
	t.Helper()

	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("failed to load config for the test database: %v", err)
	}
	baseURL := cfg.Connection.DatabaseURL

	name := fmt.Sprintf("weave_test_%d_%d", os.Getpid(), created.Add(1))

	ctx := t.Context()

	admin, err := pgx.Connect(ctx, replaceDatabase(baseURL, "postgres"))
	if err != nil {
		t.Fatalf("failed to reach postgres; is it running? try 'mise run docker:up': %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", name)); err != nil {
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

		if _, err := conn.Exec(dropCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", name)); err != nil {
			t.Logf("failed to drop test database %q: %v", name, err)
		}
	})

	return replaceDatabase(baseURL, name)
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
