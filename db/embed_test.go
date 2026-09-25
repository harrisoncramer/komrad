package db

import (
	"context"
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"komrad/internal/pgtest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrate pins that an empty database takes every migration and that a second pass over
// the same database changes nothing.
func TestMigrate(t *testing.T) {
	tests := []struct {
		name   string
		passes int
	}{
		{
			name:   "an empty database takes every migration",
			passes: 1,
		},
		{
			name:   "a second pass changes nothing",
			passes: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := open(t)
			ctx := t.Context()

			for range tt.passes {
				require.NoError(t, Migrate(ctx, pool))
			}

			assert.Equal(t, allVersions(t), appliedVersions(t, pool))
			assert.True(t, tableExists(t, pool, "agents"))
		})
	}
}

// TestMigrateConcurrently pins the advisory lock, without which a second starter reads the
// same empty ledger and reapplies what the first already has.
func TestMigrateConcurrently(t *testing.T) {
	const racers = 8

	url := pgtest.URL(t)
	ctx := t.Context()

	pools := make([]*pgxpool.Pool, racers)
	for i := range pools {
		pools[i] = openAt(t, url)
	}

	start := make(chan struct{})
	errs := make(chan error, racers)

	var wg sync.WaitGroup
	for _, pool := range pools {
		wg.Go(func() {
			<-start
			errs <- Migrate(ctx, pool)
		})
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	assert.Equal(t, allVersions(t), appliedVersions(t, pools[0]))
}

// TestMigrateSharedDatabase is the guard against a checkout migrating the database a running komrad
// is serving from.
func TestMigrateSharedDatabase(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		migrate bool
		refused bool
	}{
		{
			name:    "another komrad on the database refuses a pass with migrations to apply",
			appName: "komrad/elsewhere/1",
			refused: true,
		},
		{
			name:    "a connection of this process's own is not another komrad",
			appName: AppName(),
		},
		{
			name:    "anything else on the database is not a komrad",
			appName: "psql",
		},
		{
			name:    "a database already up to date is left alone rather than refused",
			appName: "komrad/elsewhere/1",
			migrate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := pgtest.URL(t)
			pool := openAt(t, url)
			ctx := t.Context()

			if tt.migrate {
				require.NoError(t, Migrate(ctx, pool))
			}

			other := connect(t, url)
			_, err := other.Exec(ctx, `SELECT set_config('application_name', $1, false)`, tt.appName)
			require.NoError(t, err)

			err = Migrate(ctx, pool)
			if tt.refused {
				require.ErrorIs(t, err, ErrDatabaseInUse)
				assert.Empty(t, appliedVersions(t, pool), "and applied none of them")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, allVersions(t), appliedVersions(t, pool))
		})
	}
}

// TestMigrateFailure pins that a migration failing partway applies none of itself.
func TestMigrateFailure(t *testing.T) {
	tests := []struct {
		name       string
		obstruct   string
		unobstruct string
		recorded   int
	}{
		{
			// workflow_hooks is the last table the agents migration creates.
			name:       "a migration that fails partway applies none of itself",
			obstruct:   `CREATE TABLE workflow_hooks (placeholder int)`,
			unobstruct: `DROP TABLE workflow_hooks`,
			recorded:   1,
		},
		{
			// A ledger the insert cannot satisfy is the only way to fail a migration whose
			// own statements all succeeded.
			name:       "a migration that cannot be recorded is rolled back with its row",
			obstruct:   `CREATE TABLE schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL)`,
			unobstruct: `ALTER TABLE schema_migrations DROP COLUMN applied_at`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := open(t)
			ctx := t.Context()

			_, err := pool.Exec(ctx, tt.obstruct)
			require.NoError(t, err)

			require.Error(t, Migrate(ctx, pool))
			assert.False(t, tableExists(t, pool, "agents"), "the failed migration rolled back")
			assert.ElementsMatch(t, allVersions(t)[:tt.recorded], appliedVersions(t, pool), "and recorded only what ran before it")

			_, err = pool.Exec(ctx, tt.unobstruct)
			require.NoError(t, err)

			require.NoError(t, Migrate(ctx, pool), "a failed run leaves the database resumable")
			assert.Equal(t, allVersions(t), appliedVersions(t, pool))
		})
	}
}

// TestMigrateResumes covers a pass that stops partway through a ledger of several files,
// using a set written for the test since how many files ship in db/migrations is up to
// whoever last squashed them.
func TestMigrateResumes(t *testing.T) {
	pool := open(t)
	ctx := t.Context()

	files := fstest.MapFS{
		"20260101000000_first.sql":  {Data: []byte(`CREATE TABLE resume_probe (id int PRIMARY KEY)`)},
		"20260102000000_second.sql": {Data: []byte(`ALTER TABLE resume_probe ADD COLUMN extra int`)},
	}
	versions, err := listMigrationVersions(files)
	require.NoError(t, err)
	require.Len(t, versions, 2)

	// A ledger that accepts only the first version stops the pass at the second, and the
	// version is interpolated because CREATE TABLE binds no parameters.
	_, err = pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE schema_migrations (version text PRIMARY KEY CHECK (version = '%s'))`, versions[0]))
	require.NoError(t, err)

	require.Error(t, migrate(ctx, pool, files))
	assert.Equal(t, versions[:1], appliedVersions(t, pool))
	assert.False(t, columnExists(t, pool, "resume_probe", "extra"), "the second migration did not land")

	_, err = pool.Exec(ctx, `ALTER TABLE schema_migrations DROP CONSTRAINT schema_migrations_version_check`)
	require.NoError(t, err)

	require.NoError(t, migrate(ctx, pool, files))
	assert.Equal(t, versions, appliedVersions(t, pool))
	assert.True(t, columnExists(t, pool, "resume_probe", "extra"), "and the rest picked up where it stopped")
}

// TestCheckSchemaWantsExactlyThisBuildsMigrations covers a service refusing a database behind its build and one ahead of it.
func TestCheckSchemaWantsExactlyThisBuildsMigrations(t *testing.T) {
	pool := open(t)
	ctx := t.Context()

	first := fstest.MapFS{"20260101000000_first.sql": {Data: []byte(`CREATE TABLE check_probe (id int)`)}}
	both := fstest.MapFS{
		"20260101000000_first.sql":  first["20260101000000_first.sql"],
		"20260102000000_second.sql": {Data: []byte(`ALTER TABLE check_probe ADD COLUMN extra int`)},
	}

	require.ErrorIs(t, checkSchema(ctx, pool, first), ErrSchemaBehind, "an empty database is behind every build")

	require.NoError(t, migrate(ctx, pool, first))
	require.NoError(t, checkSchema(ctx, pool, first))
	require.ErrorIs(t, checkSchema(ctx, pool, both), ErrSchemaBehind)

	require.NoError(t, migrate(ctx, pool, both))
	require.NoError(t, checkSchema(ctx, pool, both))
	require.ErrorIs(t, checkSchema(ctx, pool, first), ErrSchemaAhead)
}

// TestMigrateUpToDate holds the lock elsewhere, so a Migrate that reached for it
// would block until the deadline instead of returning.
func TestMigrateUpToDate(t *testing.T) {
	url := pgtest.URL(t)
	pool := openAt(t, url)
	ctx := t.Context()

	require.NoError(t, Migrate(ctx, pool))

	holder := connect(t, url)

	_, err := holder.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLock)
	require.NoError(t, err)

	deadlined, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	require.NoError(t, Migrate(deadlined, pool))
}

// TestMigrateReleasesLock pins that the advisory lock is free again once the migration is
// done, so the next process is not left waiting on it.
func TestMigrateReleasesLock(t *testing.T) {
	url := pgtest.URL(t)
	pool := openAt(t, url)

	require.NoError(t, Migrate(t.Context(), pool))

	var taken bool
	require.NoError(t, connect(t, url).QueryRow(t.Context(),
		`SELECT pg_try_advisory_lock($1)`, migrateLock).Scan(&taken))
	assert.True(t, taken)
}

// TestReleaseMigrateLockOnCancelledContext covers a cancel landing between two statements,
// the one path that leaves the connection healthy enough to be pooled with the lock held.
func TestReleaseMigrateLockOnCancelledContext(t *testing.T) {
	url := pgtest.URL(t)
	pool := openAt(t, url)

	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `SELECT pg_advisory_lock($1)`, migrateLock)
	require.NoError(t, err)

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	releaseMigrateLock(cancelled, conn)
	conn.Release()

	var taken bool
	require.NoError(t, connect(t, url).QueryRow(t.Context(),
		`SELECT pg_try_advisory_lock($1)`, migrateLock).Scan(&taken))
	assert.True(t, taken, "the migration lock outlived the run that took it")
}

// TestIsPending pins what a pending check reports for a database that has never been
// migrated and for one already up to date.
func TestIsPending(t *testing.T) {
	tests := []struct {
		name    string
		migrate bool
		want    bool
	}{
		{
			name:    "a database that has never been migrated has no ledger to read",
			migrate: false,
			want:    true,
		},
		{
			name:    "a database already up to date has nothing left",
			migrate: true,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := open(t)
			ctx := t.Context()

			if tt.migrate {
				require.NoError(t, Migrate(ctx, pool))
			}

			got, err := isPending(ctx, pool, allVersions(t))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMigrationsHaveNoTxmodeDirective guards the concurrent_index option in
// db/atlas.hcl, which makes Atlas tag its migrations with a directive saying not
// to run them in a transaction.
func TestMigrationsHaveNoTxmodeDirective(t *testing.T) {
	files := embeddedFiles(t)

	for _, version := range allVersions(t) {
		sqlText, err := fs.ReadFile(files, version)
		require.NoError(t, err)

		assert.NotContains(t, string(sqlText), "atlas:txmode none",
			"%s asks not to run in a transaction, which apply cannot honour, since it wraps every "+
				"file in one and sends it as a single Exec that Postgres wraps in another. Teach it to "+
				"split those files and run their statements one at a time, or the binary will not start.",
			version)
	}
}

// open builds a pool over a throwaway database, left unmigrated because applying
// the migrations is what is under test.
func open(t *testing.T) *pgxpool.Pool {
	t.Helper()

	return openAt(t, pgtest.URL(t))
}

func openAt(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	cfg.MaxConns = 2

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

// connect opens a connection of its own, an advisory lock being held by a session rather than by
// a statement.
func connect(t *testing.T, url string) *pgx.Conn {
	t.Helper()

	conn, err := pgx.Connect(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.WithoutCancel(t.Context())) })

	return conn
}

// embeddedFiles is the migration set the binary ships with.
func embeddedFiles(t *testing.T) fs.FS {
	t.Helper()

	files, err := openMigrationFiles()
	require.NoError(t, err)

	return files
}

func allVersions(t *testing.T) []string {
	t.Helper()

	versions, err := listMigrationVersions(embeddedFiles(t))
	require.NoError(t, err)
	require.NotEmpty(t, versions)

	return versions
}

// appliedVersions reads the ledger, counting a database with no ledger at all as
// one that has recorded nothing.
func appliedVersions(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()

	if !tableExists(t, pool, "schema_migrations") {
		return nil
	}

	rows, err := pool.Query(t.Context(), `SELECT version FROM schema_migrations ORDER BY version`)
	require.NoError(t, err)

	versions, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)

	return versions
}

func columnExists(t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()

	var exists bool
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = $1 AND column_name = $2)`,
		table, column).Scan(&exists))

	return exists
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()

	var exists bool
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists))

	return exists
}

// TestSchemaVersionChangesWithTheMigrations is what a template database built from an older
// schema is told apart by, so anything the migrations do differently has to move it.
func TestSchemaVersionChangesWithTheMigrations(t *testing.T) {
	base := fstest.MapFS{
		"20260101000000_first.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id int);")},
	}

	version, err := schemaVersion(base)
	require.NoError(t, err)
	require.Len(t, version, versionLength)

	moved := map[string]fs.FS{
		"a migration was edited": fstest.MapFS{
			"20260101000000_first.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id int);")},
		},
		"a migration was renamed": fstest.MapFS{
			"20260101000000_second.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id int);")},
		},
		"a migration was added": fstest.MapFS{
			"20260101000000_first.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE a (id int);")},
			"20260102000000_second.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id int);")},
		},
	}

	for name, files := range moved {
		t.Run(name, func(t *testing.T) {
			other, err := schemaVersion(files)
			require.NoError(t, err)
			assert.NotEqual(t, version, other)
		})
	}

	again, err := schemaVersion(base)
	require.NoError(t, err)
	assert.Equal(t, version, again, "the same migrations fingerprint the same twice")
}

// TestSchemaVersionReadsTheEmbeddedMigrations covers the exported call a test database is
// named after.
func TestSchemaVersionReadsTheEmbeddedMigrations(t *testing.T) {
	version, err := SchemaVersion()
	require.NoError(t, err)

	assert.Len(t, version, versionLength)
}
