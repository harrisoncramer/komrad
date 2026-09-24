// Package pgtest hands a test its own Postgres database, and skips the test rather than failing
// it when there is no server to make one on.
package pgtest

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"komrade/config"

	"github.com/jackc/pgx/v5"
)

const (
	namePrefix = "komrade_test_"

	// templateMarker separates a template's name from the version of the schema in it.
	templateMarker = "_tpl"

	// buildingMarker names a template that is still being built.
	buildingMarker = "_tplbuilding"

	// nameTimeLayout is spelled out rather than left as a count of seconds so that a pid,
	// which is what a worktree still on the old build is putting here, cannot be read as a
	// time and swept out from under a run that is very much alive.
	nameTimeLayout = "20060102t150405"

	// maxAge is deliberately far longer than the ten minutes Go allows a test binary, leaving a run
	// under a debugger or a raised timeout its own database.
	maxAge = 24 * time.Hour

	// templateLockClass is the advisory lock class the template build is serialised under.
	templateLockClass = 7240116

	// versionLength is how much of a hashed version a template's name carries.
	versionLength = 12
)

var sweepOnce sync.Once

// lookupTemplate is the name of the template holding a version of a schema, empty when the
// server has none.
var lookupTemplate = (*admin).findTemplate

// Schema is a versioned database a test's own is copied from.
type Schema struct {
	Version string
	Apply   func(ctx context.Context, url string) error
}

// URL creates an empty database and drops it when the test ends, leaving the store to
// migrate it so the tests exercise the same path a real start does.
func URL(t *testing.T) string {
	t.Helper()

	admin := connect(t)
	defer admin.close(t)

	return admin.database(t, "")
}

// Migrated creates a copy of a database Apply has already built, so a test that only needs the
// schema does not pay to build it again.
func Migrated(t *testing.T, schema Schema) string {
	t.Helper()

	admin := connect(t)
	defer admin.close(t)

	url, first := admin.copy(t, admin.template(t, schema))
	if first == nil {
		return url
	}

	// The sweep of another run can drop a template between the lookup and the copy.
	url, second := admin.copy(t, admin.buildTemplate(t, schema))
	if second != nil {
		t.Fatalf("failed to copy the template database: %v, and again after building it: %v", first, second)
	}

	return url
}

// admin is the connection new databases are created through.
type admin struct {
	conn    *pgx.Conn
	baseURL string
}

// connect opens the connection the databases of a test are created over, sweeping what earlier
// runs left behind on its way in.
func connect(t *testing.T) *admin {
	t.Helper()

	cfg, err := config.LoadStack(config.Stack{})
	if err != nil {
		t.Fatalf("failed to load config for the test database: %v", err)
	}
	baseURL := cfg.Connection.OwnerDatabaseURL

	conn, err := pgx.Connect(t.Context(), replaceDatabase(baseURL, "postgres"))
	if err != nil {
		t.Fatalf("failed to reach postgres; is it running? try 'mise run docker:up': %v", err)
	}

	opened := &admin{conn: conn, baseURL: baseURL}
	sweepOnce.Do(func() { opened.sweep(t) })

	return opened
}

func (a *admin) close(t *testing.T) {
	t.Helper()

	_ = a.conn.Close(t.Context())
}

// database creates a database of this test's own, copied from template unless it is empty.
func (a *admin) database(t *testing.T, template string) string {
	t.Helper()

	url, err := a.copy(t, template)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}

	return url
}

// copy creates a database of this test's own and hands back its url, dropping it when the test
// ends.
func (a *admin) copy(t *testing.T, template string) (string, error) {
	t.Helper()

	name := newName("")
	if err := a.create(t.Context(), name, template); err != nil {
		return "", err
	}

	a.dropWhenDone(t, name)

	return replaceDatabase(a.baseURL, name), nil
}

// create makes a database, copying template when one is named.
func (a *admin) create(ctx context.Context, name, template string) error {
	statement := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize()
	if template != "" {
		statement += " TEMPLATE " + pgx.Identifier{template}.Sanitize()
	}

	_, err := a.conn.Exec(ctx, statement)

	return err
}

// dropWhenDone drops a database when the test ends, over a connection of its own.
func (a *admin) dropWhenDone(t *testing.T, name string) {
	t.Helper()

	t.Cleanup(func() {
		// The test's own context is already cancelled by the time a cleanup runs, and the
		// drop still has to reach the server.
		dropCtx := context.Background() //nolint:usetesting // as above

		conn, err := pgx.Connect(dropCtx, replaceDatabase(a.baseURL, "postgres"))
		if err != nil {
			t.Logf("failed to reach postgres to drop %q: %v", name, err)
			return
		}
		defer func() { _ = conn.Close(dropCtx) }()

		if err := drop(dropCtx, conn, name); err != nil {
			t.Logf("failed to drop test database %q: %v", name, err)
		}
	})
}

// template is the template database holding this schema, built if this is the first run to ask
// for it.
func (a *admin) template(t *testing.T, schema Schema) string {
	t.Helper()

	name, err := lookupTemplate(a, t.Context(), schema.Version)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if name != "" {
		return name
	}

	return a.buildTemplate(t, schema)
}

// buildTemplate builds the template under an advisory lock.
func (a *admin) buildTemplate(t *testing.T, schema Schema) string {
	t.Helper()

	ctx := t.Context()
	key := lockKey(schema.Version)

	if _, err := a.conn.Exec(ctx, "SELECT pg_advisory_lock($1, $2)", templateLockClass, key); err != nil {
		t.Fatalf("failed to take the template lock: %v", err)
	}
	defer func() {
		_, _ = a.conn.Exec(ctx, "SELECT pg_advisory_unlock($1, $2)", templateLockClass, key)
	}()

	built, err := lookupTemplate(a, ctx, schema.Version)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if built != "" {
		return built
	}

	building := newName(buildingMarker)
	if err := a.create(ctx, building, ""); err != nil {
		t.Fatalf("failed to create the template database %q: %v", building, err)
	}

	if err := schema.Apply(ctx, replaceDatabase(a.baseURL, building)); err != nil {
		t.Fatalf("failed to build the template database %q: %v", building, err)
	}

	// Naming it last is what makes it safe to copy: a build that dies part-way leaves a
	// database no lookup will find, and the sweep collects it.
	name := templateName(schema.Version)
	rename := "ALTER DATABASE " + pgx.Identifier{building}.Sanitize() +
		" RENAME TO " + pgx.Identifier{name}.Sanitize()
	if _, err := a.conn.Exec(ctx, rename); err != nil {
		t.Fatalf("failed to name the template database %q: %v", name, err)
	}

	a.protect(ctx, name)

	return name
}

// findTemplate is the name of the template holding a version of the schema, empty when the
// server has none.
func (a *admin) findTemplate(ctx context.Context, version string) (string, error) {
	suffix := templateSuffix(version)

	rows, err := a.conn.Query(ctx,
		`SELECT datname FROM pg_database
		 WHERE starts_with(datname, $1) AND right(datname, length($2::text)) = $2::text`,
		namePrefix, suffix)
	if err != nil {
		return "", fmt.Errorf("failed to look for the template database: %w", err)
	}

	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", fmt.Errorf("failed to read the template databases: %w", err)
	}
	if len(names) == 0 {
		return "", nil
	}

	return names[0], nil
}

// protect closes a built template to connections.
func (a *admin) protect(ctx context.Context, name string) {
	// It needs a superuser and is worth nothing more than the connection it refuses, so a
	// server that says no is left as it is.
	_, _ = a.conn.Exec(ctx,
		`UPDATE pg_database SET datistemplate = true, datallowconn = false WHERE datname = $1`, name)
}

// sweep drops the test databases left behind by runs that were killed before reaching their
// cleanup, logging what it cannot drop rather than failing a test over housekeeping.
func (a *admin) sweep(t *testing.T) {
	t.Helper()

	ctx := t.Context()

	rows, err := a.conn.Query(ctx, "SELECT datname FROM pg_database WHERE starts_with(datname, $1)", namePrefix)
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
		if err := drop(ctx, a.conn, name); err != nil {
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

// newName is a database name carrying the time it was made, ending in suffix when one is given.
func newName(suffix string) string {
	name := fmt.Sprintf("%s%s_%016x", namePrefix, time.Now().UTC().Format(nameTimeLayout), rand.Uint64())
	if suffix == "" {
		return name
	}

	return name + suffix
}

// templateName is the name a template of this version of the schema is found under.
func templateName(version string) string {
	return newName(templateSuffix(version))
}

// templateSuffix ends the name of every template built from a version of a schema.
func templateSuffix(version string) string {
	sum := sha256.Sum256([]byte(version))

	return templateMarker + hex.EncodeToString(sum[:])[:versionLength]
}

// lockKey is the advisory lock a version of the schema is built under.
func lockKey(version string) int32 {
	sum := sha256.Sum256([]byte(version))

	return int32(binary.BigEndian.Uint32(sum[:4]))
}

func drop(ctx context.Context, conn *pgx.Conn, name string) error {
	statement := "DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"

	_, err := conn.Exec(ctx, statement)
	if err == nil {
		return nil
	}

	// Postgres refuses to drop a template until it is no longer marked as one.
	if _, clearErr := conn.Exec(ctx,
		`UPDATE pg_database SET datistemplate = false WHERE datname = $1`, name); clearErr != nil {
		return err
	}

	_, err = conn.Exec(ctx, statement)

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
