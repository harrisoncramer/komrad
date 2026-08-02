package doctor

import (
	"testing"

	"weave/db"
	"weave/internal/pgtest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProbeLeavesTheSchemaAlone is the regression that matters about this whole command.
//
// store.Open applies the migrations on its way in, so the ordinary way of reaching the
// database would have doctor migrating whatever it was asked to report on. A schema check
// that fixes the schema can never tell you it was behind, and a diagnostic run against a
// production database must not be the thing that changes it.
func TestProbeLeavesTheSchemaAlone(t *testing.T) {
	url := pgtest.URL(t)

	handle, err := connectProbe(t.Context(), url)
	require.NoError(t, err)
	defer handle.Close()

	pending, err := handle.Pending(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, pending, "a database pgtest just created has every migration outstanding")

	inspect, err := pgxpool.New(t.Context(), url)
	require.NoError(t, err)
	defer inspect.Close()

	var tables int
	require.NoError(t, inspect.QueryRow(t.Context(),
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'`).Scan(&tables))

	assert.Zero(t, tables, "reading the schema created nothing, not even the migration ledger")
}

// TestProbeCannotWrite covers the other half of the promise. The read-only flag rides on
// the connection, so a statement that would write is refused by Postgres rather than by
// this package remembering not to send one.
func TestProbeCannotWrite(t *testing.T) {
	url := pgtest.URL(t)

	handle, err := connectProbe(t.Context(), url)
	require.NoError(t, err)
	defer handle.Close()

	probe, ok := handle.(*pgProbe)
	require.True(t, ok)

	_, err = probe.pool.Exec(t.Context(), `CREATE TABLE written_anyway (id int)`)
	require.Error(t, err, "the connection refuses a write without the caller having to avoid one")
}

// TestProbeReportsNothingPendingOnceMigrated covers the passing answer, so the check cannot
// be reading "up to date" off a query that always comes back empty.
func TestProbeReportsNothingPendingOnceMigrated(t *testing.T) {
	url := pgtest.URL(t)

	migrated, err := pgxpool.New(t.Context(), url)
	require.NoError(t, err)
	defer migrated.Close()
	require.NoError(t, db.Migrate(t.Context(), migrated))

	handle, err := connectProbe(t.Context(), url)
	require.NoError(t, err)
	defer handle.Close()

	pending, err := handle.Pending(t.Context())
	require.NoError(t, err)
	assert.Empty(t, pending)
}
