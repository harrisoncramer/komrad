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
	"komrad/lib/xslice"

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
			assert.Equal(t, embeddedUnits(t), appliedUnits(t, pool))
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

	require.Error(t, migrate(ctx, pool, migrationsOnly(files)))
	assert.Equal(t, versions[:1], appliedVersions(t, pool))
	assert.False(t, columnExists(t, pool, "resume_probe", "extra"), "the second migration did not land")

	_, err = pool.Exec(ctx, `ALTER TABLE schema_migrations DROP CONSTRAINT schema_migrations_version_check`)
	require.NoError(t, err)

	require.NoError(t, migrate(ctx, pool, migrationsOnly(files)))
	assert.Equal(t, versions, appliedVersions(t, pool))
	assert.True(t, columnExists(t, pool, "resume_probe", "extra"), "and the rest picked up where it stopped")
}

// TestCheckSchemaRefusesOnlyADatabaseBehind pins that an older build still starts against a schema a newer build migrated.
func TestCheckSchemaRefusesOnlyADatabaseBehind(t *testing.T) {
	pool := open(t)
	ctx := t.Context()

	first := fstest.MapFS{"20260101000000_first.sql": {Data: []byte(`CREATE TABLE check_probe (id int)`)}}
	both := fstest.MapFS{
		"20260101000000_first.sql":  first["20260101000000_first.sql"],
		"20260102000000_second.sql": {Data: []byte(`ALTER TABLE check_probe ADD COLUMN extra int`)},
	}

	require.ErrorIs(t, checkSchema(ctx, pool, migrationsOnly(first)), ErrSchemaBehind, "an empty database is behind every build")

	require.NoError(t, migrate(ctx, pool, migrationsOnly(first)))
	require.NoError(t, checkSchema(ctx, pool, migrationsOnly(first)))
	require.ErrorIs(t, checkSchema(ctx, pool, migrationsOnly(both)), ErrSchemaBehind)

	require.NoError(t, migrate(ctx, pool, migrationsOnly(both)))
	require.NoError(t, checkSchema(ctx, pool, migrationsOnly(both)))
	require.NoError(t, checkSchema(ctx, pool, migrationsOnly(first)), "an older build runs against a newer schema")
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

			got, err := isPending(ctx, pool, embeddedContents(t))
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

	src, err := openSources()
	require.NoError(t, err)

	return src.migrations
}

func embeddedContents(t *testing.T) *contents {
	t.Helper()

	src, err := openSources()
	require.NoError(t, err)

	all, err := src.list()
	require.NoError(t, err)

	return all
}

func migrationsOnly(files fs.FS) *sources {
	return &sources{migrations: files, triggers: fstest.MapFS{}}
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

	version, err := schemaVersion(migrationsOnly(base))
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
			other, err := schemaVersion(migrationsOnly(files))
			require.NoError(t, err)
			assert.NotEqual(t, version, other)
		})
	}

	again, err := schemaVersion(migrationsOnly(base))
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

const (
	probeTable    = `CREATE TABLE unit_probe (id int, seen int NOT NULL DEFAULT 0)`
	probeFunction = `CREATE OR REPLACE FUNCTION mark_seen() RETURNS trigger AS $$ BEGIN NEW.seen := 1; RETURN NEW; END; $$ LANGUAGE plpgsql;`
	probeTrigger  = `CREATE OR REPLACE TRIGGER mark_seen BEFORE INSERT ON unit_probe FOR EACH ROW EXECUTE FUNCTION mark_seen();`
)

func probeSources(migrations, triggers fstest.MapFS) *sources {
	return &sources{migrations: migrations, triggers: triggers}
}

// TestMigrateTriggerUnits walks one table's triggers through being attached, versioned, reattached and forgotten.
func TestMigrateTriggerUnits(t *testing.T) {
	pool := open(t)
	ctx := t.Context()

	migrations := fstest.MapFS{"20260101000000_first.sql": {Data: []byte(probeTable)}}
	attached := fstest.MapFS{
		"_functions.v1.sql": {Data: []byte(probeFunction)},
		"unit_probe.v1.sql": {Data: []byte(probeTrigger)},
	}

	require.NoError(t, migrate(ctx, pool, probeSources(migrations, attached)))
	assert.Equal(t, map[string]int{"_functions": 1, "unit_probe": 1}, appliedUnits(t, pool))
	assert.True(t, triggerExists(t, pool, "unit_probe", "mark_seen"))

	recreated := fstest.MapFS{
		"20260101000000_first.sql":  migrations["20260101000000_first.sql"],
		"20260102000000_second.sql": {Data: []byte(`DROP TABLE unit_probe; ` + probeTable)},
	}
	broken := fstest.MapFS{
		"_functions.v1.sql": attached["_functions.v1.sql"],
		"unit_probe.v1.sql": {Data: []byte(`SELECT 1/0;`)},
	}
	require.Error(t, migrate(ctx, pool, probeSources(recreated, broken)))
	require.ErrorIs(t, checkSchema(ctx, pool, probeSources(recreated, attached)), ErrSchemaBehind, "a unit owed by a migration stays owed after a failed run")
	require.NoError(t, migrate(ctx, pool, probeSources(recreated, attached)))
	assert.True(t, triggerExists(t, pool, "unit_probe", "mark_seen"), "a migration that rebuilds a table gets its triggers back")

	detached := fstest.MapFS{
		"_functions.v1.sql": attached["_functions.v1.sql"],
		"unit_probe.v2.sql": {Data: []byte(`DROP TRIGGER IF EXISTS mark_seen ON unit_probe;`)},
	}
	require.ErrorIs(t, checkSchema(ctx, pool, probeSources(recreated, detached)), ErrSchemaBehind)
	require.NoError(t, migrate(ctx, pool, probeSources(recreated, detached)))
	assert.Equal(t, map[string]int{"_functions": 1, "unit_probe": 2}, appliedUnits(t, pool))
	assert.False(t, triggerExists(t, pool, "unit_probe", "mark_seen"))
	assert.False(t, functionExists(t, pool, "mark_seen"), "a function no trigger runs is dropped")
	require.NoError(t, checkSchema(ctx, pool, probeSources(recreated, attached)), "an older build runs against newer triggers")

	dropped := fstest.MapFS{
		"20260101000000_first.sql":  recreated["20260101000000_first.sql"],
		"20260102000000_second.sql": recreated["20260102000000_second.sql"],
		"20260103000000_third.sql":  {Data: []byte(`DROP TABLE unit_probe`)},
	}
	functionsOnly := fstest.MapFS{"_functions.v1.sql": attached["_functions.v1.sql"]}
	require.NoError(t, migrate(ctx, pool, probeSources(dropped, functionsOnly)))
	assert.Equal(t, map[string]int{"_functions": 1}, appliedUnits(t, pool), "a unit no longer carried leaves the ledger")
}

// TestListUnits pins the order units apply in and the file names it refuses.
func TestListUnits(t *testing.T) {
	tests := []struct {
		name    string
		files   fstest.MapFS
		want    []string
		wantErr string
	}{
		{
			name: "the functions come before every table",
			files: fstest.MapFS{
				"agents.v2.sql":     {},
				"_functions.v1.sql": {},
				"accounts.v1.sql":   {},
				"README.md":         {},
			},
			want: []string{"_functions.v1.sql", "accounts.v1.sql", "agents.v2.sql"},
		},
		{
			name:    "a file without a version",
			files:   fstest.MapFS{"agents.sql": {}},
			wantErr: "is not named unit.vN.sql",
		},
		{
			name:    "a version of zero",
			files:   fstest.MapFS{"agents.v0.sql": {}},
			wantErr: "has no version above zero",
		},
		{
			name:    "two files for one unit",
			files:   fstest.MapFS{"agents.v1.sql": {}, "agents.v2.sql": {}},
			wantErr: "has two files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			units, err := listUnits(tt.files)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, xslice.Map(units, func(u unit) string { return u.file }))
		})
	}
}

// TestSchemaVersionChangesWithTheTriggers pins that a template database is rebuilt when any unit changes.
func TestSchemaVersionChangesWithTheTriggers(t *testing.T) {
	migrations := fstest.MapFS{"20260101000000_first.sql": {Data: []byte(probeTable)}}
	base := fstest.MapFS{"unit_probe.v1.sql": {Data: []byte(probeTrigger)}}

	version, err := schemaVersion(probeSources(migrations, base))
	require.NoError(t, err)

	moved := map[string]fstest.MapFS{
		"a unit was edited":  {"unit_probe.v1.sql": {Data: []byte(`SELECT 1;`)}},
		"a unit was bumped":  {"unit_probe.v2.sql": {Data: []byte(probeTrigger)}},
		"a unit was added":   {"unit_probe.v1.sql": {Data: []byte(probeTrigger)}, "_functions.v1.sql": {Data: []byte(probeFunction)}},
		"a unit was removed": {},
	}

	for name, triggers := range moved {
		t.Run(name, func(t *testing.T) {
			other, err := schemaVersion(probeSources(migrations, triggers))
			require.NoError(t, err)
			assert.NotEqual(t, version, other)
		})
	}
}

func embeddedUnits(t *testing.T) map[string]int {
	t.Helper()

	units := map[string]int{}
	for _, u := range embeddedContents(t).units {
		units[u.name] = u.version
	}
	require.NotEmpty(t, units)

	return units
}

func appliedUnits(t *testing.T, pool *pgxpool.Pool) map[string]int {
	t.Helper()

	applied, err := listAppliedTriggers(t.Context(), pool)
	require.NoError(t, err)

	return applied
}

func triggerExists(t *testing.T, pool *pgxpool.Pool, table, name string) bool {
	t.Helper()

	var exists bool
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = to_regclass($1) AND tgname = $2)`,
		table, name).Scan(&exists))

	return exists
}

func functionExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()

	var exists bool
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT to_regproc($1) IS NOT NULL`, name).Scan(&exists))

	return exists
}
