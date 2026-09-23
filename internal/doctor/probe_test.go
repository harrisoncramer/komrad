package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"weave/db"
	"weave/internal/pgtest"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProbeLeavesTheSchemaAlone is the regression that matters about this whole command:
// store.Open applies migrations on its way in, so the ordinary way of reaching the database
// would have doctor migrating whatever it was asked to report on, and a diagnostic run
// against a production database must not be the thing that changes it.
func TestProbeLeavesTheSchemaAlone(t *testing.T) {
	url := pgtest.URL(t)

	pending, err := readPendingMigrations(t.Context(), url)
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

// TestProbeCannotWrite covers the other half of the promise: the read-only flag rides on
// the connection, so a write is refused by Postgres rather than by this package remembering
// not to send one.
func TestProbeCannotWrite(t *testing.T) {
	pool, err := openReadOnlyPool(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(t.Context(), `CREATE TABLE written_anyway (id int)`)
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

	pending, err := readPendingMigrations(t.Context(), url)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

// TestInspectDirReadsADirectoryApartFromAFile covers the three answers the directory checks turn
// on.
func TestInspectDirReadsADirectoryApartFromAFile(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "trees", "01a0"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "trees", "notify"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "trees", ".DS_Store"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "afile"), nil, 0o644))

	state, err := inspectDir(filepath.Join(root, "trees"))
	require.NoError(t, err)
	assert.True(t, state.Present)
	assert.True(t, state.Directory)
	assert.True(t, state.Writable)
	assert.Equal(t, []string{"01a0"}, state.Directories)
	assert.Equal(t, []string{"notify"}, state.Files, "a dotfile is not a hook")

	state, err = inspectDir(filepath.Join(root, "afile"))
	require.NoError(t, err)
	assert.True(t, state.Present)
	assert.False(t, state.Directory)

	state, err = inspectDir(filepath.Join(root, "nothing"))
	require.NoError(t, err)
	assert.False(t, state.Present, "a path with nothing at it is reported rather than raised")
}

// TestInspectDirReportsADirectoryItCannotWrite covers the answer the worktrees check fails on.
func TestInspectDirReportsADirectoryItCannotWrite(t *testing.T) {
	root := t.TempDir()

	locked := filepath.Join(root, "locked")
	require.NoError(t, os.Mkdir(locked, 0o500))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	state, err := inspectDir(locked)
	require.NoError(t, err)
	assert.True(t, state.Directory)
	assert.False(t, state.Writable)
}

// TestReadWorktreeOwnersReadsTheAgentsAndTheWorkflowDirs covers what makes an orphan
// answerable: the ids weave names its worktrees after, the directories agents actually run
// in, and the places the workflows cut their trees.
func TestReadWorktreeOwnersReadsTheAgentsAndTheWorkflowDirs(t *testing.T) {
	stores := storetest.Open(t)

	workflow := stores.SeedWorkflow(t, workflowstore.Config{
		Name:     "trees",
		Worktree: workflowstore.WorktreeConfig{Enabled: true, Dir: "~/repos/trees"},
	})
	agent := stores.SeedAgent(t, agentstore.Agent{
		Name:       "worker",
		Cwd:        "/repos/trees/worker",
		WorkflowID: workflow.ID,
	})

	owners, err := readWorktreeOwners(t.Context(), stores.URL)
	require.NoError(t, err)

	assert.Contains(t, owners.AgentIDs, agent.ID)
	assert.Contains(t, owners.AgentDirs, "/repos/trees/worker")
	assert.Equal(t, []string{"~/repos/trees"}, owners.WorkflowDirs,
		"the directory is reported as the workflow named it, which is what the config is matched against")
}

// TestReadWorktreeOwnersFailsWithoutTheTables covers the answer the orphan check degrades on,
// where a database that cannot be read is not a machine with no orphans.
func TestReadWorktreeOwnersFailsWithoutTheTables(t *testing.T) {
	_, err := readWorktreeOwners(t.Context(), pgtest.URL(t))

	require.Error(t, err, "an unmigrated database says so rather than reporting nothing owns anything")
}
