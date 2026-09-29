// Package dbtest hands a test a database with komrad's schema already in it, copied from a
// template rather than migrated from empty, and an engine of the test's own to connect as.
package dbtest

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"net/url"
	"sync"
	"testing"

	"komrad/db"
	"komrad/internal/pgtest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

//go:embed drop_engine.sql
var dropEngineSQL string

//go:embed engine_login.sql
var engineLoginSQL string

//go:embed sweep_engines.sql
var sweepEnginesSQL string

var sweepOnce sync.Once

// Database is a migrated database of this test's own.
type Database struct {
	OwnerURL string
}

// Engine is one engine on a test's database, with the urls its two logins connect with.
type Engine struct {
	Name      string
	URL       string
	ReaderURL string
}

// URL is an engine's login on a migrated database of this test's own, both dropped when the test ends.
func URL(t *testing.T) string {
	t.Helper()

	return NewEngine(t, New(t)).URL
}

// New is a migrated database of this test's own, dropped when the test ends.
func New(t *testing.T) Database {
	t.Helper()

	version, err := db.SchemaVersion()
	require.NoError(t, err)

	return Database{OwnerURL: pgtest.Migrated(t, pgtest.Schema{Version: version, Apply: migrate})}
}

// NewEngine adds an engine to database, its logins dropped when the test ends.
func NewEngine(t *testing.T, database Database) Engine {
	t.Helper()

	ctx := t.Context()
	owner, err := pgxpool.New(ctx, database.OwnerURL)
	require.NoError(t, err)
	defer owner.Close()

	sweepOnce.Do(func() {
		_, err := owner.Exec(ctx, sweepEnginesSQL)
		require.NoError(t, err)
	})

	name := EngineName(t, database)
	password := randomHex(t, 16)
	require.NoError(t, db.EnsureEngine(ctx, owner, db.EnsureEngineParams{Name: name, Password: password}))

	return Engine{
		Name:      name,
		URL:       database.EngineURL(t, name, password),
		ReaderURL: withLogin(t, database.OwnerURL, db.EngineReaderLogin(name), password),
	}
}

// EngineName is a fresh engine name whose logins are dropped when the test ends, whoever creates them.
func EngineName(t *testing.T, database Database) string {
	t.Helper()

	name := "t" + randomHex(t, 12)
	DropEngineWhenDone(t, database, name)

	return name
}

// DropEngineWhenDone drops an engine's logins and whatever they own in database when the test ends.
func DropEngineWhenDone(t *testing.T, database Database, name string) {
	t.Helper()

	t.Cleanup(func() { dropEngine(t, database.OwnerURL, db.EngineLogin(name)) })
}

// EngineURL is the url an engine's login connects to this database with.
func (d Database) EngineURL(t *testing.T, name, password string) string {
	t.Helper()

	return withLogin(t, d.OwnerURL, db.EngineLogin(name), password)
}

// migrate applies the embedded migrations to the database url names.
func migrate(ctx context.Context, url string) error {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()

	return db.Migrate(ctx, pool)
}

func dropEngine(t *testing.T, ownerURL, login string) {
	t.Helper()

	// The test's own context is already cancelled by the time a cleanup runs.
	ctx := context.WithoutCancel(t.Context())

	owner, err := pgxpool.New(ctx, ownerURL)
	if err != nil {
		t.Logf("failed to reach postgres to drop %s: %v", login, err)
		return
	}
	defer owner.Close()

	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Logf("failed to drop %s: %v", login, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, engineLoginSQL, login); err != nil {
		t.Logf("failed to drop %s: %v", login, err)
		return
	}
	if _, err := tx.Exec(ctx, dropEngineSQL); err != nil {
		t.Logf("failed to drop %s: %v", login, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		t.Logf("failed to drop %s: %v", login, err)
	}
}

func withLogin(t *testing.T, raw, user, password string) string {
	t.Helper()

	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	parsed.User = url.UserPassword(user, password)

	return parsed.String()
}

func randomHex(t *testing.T, size int) string {
	t.Helper()

	buf := make([]byte, size/2)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}
