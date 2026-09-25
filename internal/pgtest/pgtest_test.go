package pgtest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigratedBuildsTheTemplateOnce pins that the second database of a schema is a copy rather
// than another build.
func TestMigratedBuildsTheTemplateOnce(t *testing.T) {
	var builds atomic.Int64
	schema := markerSchema(t, &builds)

	first := Migrated(t, schema)
	second := Migrated(t, schema)

	assert.Equal(t, int64(1), builds.Load(), "the second database was copied from the first's template")
	assert.True(t, hasMarker(t, first))
	assert.True(t, hasMarker(t, second))
	assert.Len(t, templates(t, schema.Version), 1)
}

// TestMigratedRebuildsForANewVersion covers a branch adding a migration, where a template built
// from the schema before it must not be handed out.
func TestMigratedRebuildsForANewVersion(t *testing.T) {
	var builds atomic.Int64
	schema := markerSchema(t, &builds)
	Migrated(t, schema)

	moved := markerSchema(t, &builds)
	Migrated(t, moved)

	assert.Equal(t, int64(2), builds.Load(), "a version nothing was built for is built")
	assert.Len(t, templates(t, schema.Version), 1)
	assert.Len(t, templates(t, moved.Version), 1)
}

// TestMigratedBuildsAgainWhenTheTemplateIsGone covers the ordinary path after a sweep, where
// the lookup finds nothing and the schema is built a second time.
func TestMigratedBuildsAgainWhenTheTemplateIsGone(t *testing.T) {
	var builds atomic.Int64
	schema := markerSchema(t, &builds)
	Migrated(t, schema)

	admin := connect(t)
	defer admin.close(t)

	for _, name := range templates(t, schema.Version) {
		require.NoError(t, drop(t.Context(), admin.conn, name))
	}

	copied := Migrated(t, schema)

	assert.Equal(t, int64(2), builds.Load(), "a template that is gone is built again")
	assert.True(t, hasMarker(t, copied))
}

// TestMigratedRebuildsATemplateSweptAfterTheLookup opens the window the retry in Migrated
// exists for, where the name a lookup returned names nothing by the time it is copied.
func TestMigratedRebuildsATemplateSweptAfterTheLookup(t *testing.T) {
	var builds atomic.Int64
	schema := markerSchema(t, &builds)
	Migrated(t, schema)

	admin := connect(t)
	defer admin.close(t)

	swept := templates(t, schema.Version)
	require.Len(t, swept, 1)
	require.NoError(t, drop(t.Context(), admin.conn, swept[0]))

	nameOnce(t, swept[0])

	copied := Migrated(t, schema)

	assert.Equal(t, int64(2), builds.Load(), "the copy of a swept template is built again")
	assert.True(t, hasMarker(t, copied))
}

// TestMigratedReusesATemplateNamedByALongVersion is the regression for a version long enough
// that the name Postgres kept was not the name anything looked for.
func TestMigratedReusesATemplateNamedByALongVersion(t *testing.T) {
	var builds atomic.Int64
	schema := markerSchemaVersioned(t, strings.Repeat("v", 200)+fmt.Sprintf("%x", rand.Uint64()), &builds)

	Migrated(t, schema)
	second := Migrated(t, schema)

	assert.Equal(t, int64(1), builds.Load(), "the long version names one template, found again")
	assert.True(t, hasMarker(t, second))
}

// TestURLIsUnmigrated pins the other door, which the tests of the migrations and of komrad doctor
// need to be handed nothing at all.
func TestURLIsUnmigrated(t *testing.T) {
	conn, err := pgx.Connect(t.Context(), URL(t))
	require.NoError(t, err)
	defer func() { _ = conn.Close(t.Context()) }()

	var tables int
	require.NoError(t, conn.QueryRow(t.Context(),
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'`).Scan(&tables))

	assert.Zero(t, tables)
}

// TestLeakedTakesOnlyTheDatabasesPastMaxAge covers a template being swept on age like any
// other database.
func TestLeakedTakesOnlyTheDatabasesPastMaxAge(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * maxAge).UTC().Format(nameTimeLayout)
	recent := now.Add(-time.Minute).UTC().Format(nameTimeLayout)

	names := []string{
		namePrefix + old + "_0f",
		namePrefix + recent + "_0f",
		namePrefix + old + "_0f" + templateSuffix("abc"),
		"komrad",
	}

	assert.Equal(t, []string{names[0], names[2]}, leaked(names, now))
}

// nameOnce hands the next lookup a name of the caller's choosing, and every lookup after that
// the real answer.
func nameOnce(t *testing.T, name string) {
	t.Helper()

	lookup := lookupTemplate
	var served atomic.Bool

	lookupTemplate = func(a *admin, ctx context.Context, version string) (string, error) {
		if served.CompareAndSwap(false, true) {
			return name, nil
		}

		return lookup(a, ctx, version)
	}

	t.Cleanup(func() { lookupTemplate = lookup })
}

// markerSchema is a schema of its own, holding one table a copy of it can be recognised by.
func markerSchema(t *testing.T, builds *atomic.Int64) Schema {
	t.Helper()

	return markerSchemaVersioned(t, fmt.Sprintf("v%x", rand.Uint64()), builds)
}

// markerSchemaVersioned is the same schema under a version the caller picks.
func markerSchemaVersioned(t *testing.T, version string, builds *atomic.Int64) Schema {
	t.Helper()

	// The connection is opened here rather than in the cleanup, where the test's own context
	// is already cancelled.
	admin := connect(t)

	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		defer func() { _ = admin.conn.Close(ctx) }()

		for {
			name, err := admin.findTemplate(ctx, version)
			if err != nil {
				t.Logf("failed to look for the templates of %q: %v", version, err)
				return
			}
			if name == "" {
				return
			}

			if err := drop(ctx, admin.conn, name); err != nil {
				t.Logf("failed to drop the template %q: %v", name, err)
				return
			}
		}
	})

	return Schema{
		Version: version,
		Apply: func(ctx context.Context, url string) error {
			builds.Add(1)

			conn, err := pgx.Connect(ctx, url)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close(ctx) }()

			_, err = conn.Exec(ctx, `CREATE TABLE marker (id int)`)

			return err
		},
	}
}

// templates is the templates the server holds for a version of a schema.
func templates(t *testing.T, version string) []string {
	t.Helper()

	admin := connect(t)
	defer admin.close(t)

	rows, err := admin.conn.Query(t.Context(),
		`SELECT datname FROM pg_database
		 WHERE starts_with(datname, $1) AND right(datname, length($2::text)) = $2::text`,
		namePrefix, templateSuffix(version))
	require.NoError(t, err)

	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)

	return names
}

// hasMarker reports whether the database url names was copied from a marker schema.
func hasMarker(t *testing.T, url string) bool {
	t.Helper()

	conn, err := pgx.Connect(t.Context(), url)
	require.NoError(t, err)
	defer func() { _ = conn.Close(t.Context()) }()

	var found bool
	require.NoError(t, conn.QueryRow(t.Context(),
		`SELECT to_regclass('marker') IS NOT NULL`).Scan(&found))

	return found
}
