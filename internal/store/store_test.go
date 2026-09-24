package store_test

import (
	"testing"

	"weave/db"
	"weave/db/models"
	"weave/internal/dbtest"
	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openEngine(t *testing.T, engine dbtest.Engine) *store.Pool {
	t.Helper()

	pool, err := store.Open(t.Context(), engine.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return pool
}

// TestEnginesSharingADatabaseSeeOnlyTheirOwnRows pins the boundary from both sides, including the reader login.
func TestEnginesSharingADatabaseSeeOnlyTheirOwnRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := dbtest.New(t)
	first := dbtest.NewEngine(t, database)
	second := dbtest.NewEngine(t, database)

	firstPool := openEngine(t, first)
	secondPool := openEngine(t, second)

	created, err := workflowstore.NewWorkflowStore(firstPool.GetDB()).Create(ctx, workflowstore.Config{Name: "shared-name", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)

	mine, err := workflowstore.NewWorkflowStore(firstPool.GetDB()).List(ctx)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, created.ID, mine[0].ID)

	theirs, err := workflowstore.NewWorkflowStore(secondPool.GetDB()).List(ctx)
	require.NoError(t, err)
	assert.Empty(t, theirs, "another engine's workflow is out of reach")

	_, err = workflowstore.NewWorkflowStore(secondPool.GetDB()).Create(ctx, workflowstore.Config{Name: "shared-name", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err, "a name one engine holds is free for another")

	reader, err := store.OpenReadOnly(ctx, second.ReaderURL, secondPool.GetEngine().ID)
	require.NoError(t, err)
	t.Cleanup(reader.Close)

	read, err := workflowstore.NewWorkflowStore(reader).List(ctx)
	require.NoError(t, err)
	require.Len(t, read, 1, "the reader login sees its own engine's rows and no other's")
	assert.NotEqual(t, created.ID, read[0].ID)
}

// TestARowIsStampedWithTheEngineThatWroteIt pins the stamp coming from the login rather than the query.
func TestARowIsStampedWithTheEngineThatWroteIt(t *testing.T) {
	t.Parallel()

	engine := dbtest.NewEngine(t, dbtest.New(t))
	pool := openEngine(t, engine)

	config, err := models.New(pool.GetDB()).InsertWorkflowConfig(t.Context(), models.InsertWorkflowConfigParams{Harness: "claude"})
	require.NoError(t, err)

	assert.Equal(t, pool.GetEngine().ID, config.TenantID)
	assert.Equal(t, engine.Name, pool.GetEngine().Name)
}

// TestAnEngineCannotPointAtAnotherEnginesRow pins the foreign keys carrying the engine, which row security alone does not.
func TestAnEngineCannotPointAtAnotherEnginesRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := dbtest.New(t)
	firstPool := openEngine(t, dbtest.NewEngine(t, database))
	secondPool := openEngine(t, dbtest.NewEngine(t, database))

	firstQueries := models.New(firstPool.GetDB())
	config, err := firstQueries.InsertWorkflowConfig(ctx, models.InsertWorkflowConfigParams{Harness: "claude"})
	require.NoError(t, err)
	provisioner, err := firstQueries.InsertWorkflowProvisionerConfig(ctx, models.InsertWorkflowProvisionerConfigParams{
		WorktreeBaseBranch:   "main",
		WorktreeCopyPaths:    []string{},
		WorktreeSymlinkPaths: []string{},
		WorktreeDiffExclude:  []string{},
	})
	require.NoError(t, err)

	_, err = models.New(secondPool.GetDB()).InsertWorkflow(ctx, models.InsertWorkflowParams{
		Name:                "borrowed",
		WorkflowConfigID:    config.ID,
		ProvisionerConfigID: provisioner.ID,
	})

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23503", pgErr.Code, "a foreign key violation, since the other engine's row does not exist for this one")
}

// TestALoginOfNoEngineIsRefused pins the database owner, which belongs to no engine, being unable to open a pool.
func TestALoginOfNoEngineIsRefused(t *testing.T) {
	t.Parallel()

	_, err := store.Open(t.Context(), dbtest.New(t).OwnerURL)

	require.ErrorIs(t, err, store.ErrNotAnEngine)
}

// TestEnsuringAnEngineAgainKeepsItsRowsAndMovesItsPassword pins ensure as the way a rebuilt box gets back to its rows.
func TestEnsuringAnEngineAgainKeepsItsRowsAndMovesItsPassword(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := dbtest.New(t)
	name := dbtest.EngineName(t, database)

	require.NoError(t, store.EnsureEngine(ctx, database.OwnerURL, db.EnsureEngineParams{Name: name, Password: "first"}))
	before, err := store.Open(ctx, database.EngineURL(t, name, "first"))
	require.NoError(t, err)
	created, err := workflowstore.NewWorkflowStore(before.GetDB()).Create(ctx, workflowstore.Config{
		Name:     "kept",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
	})
	require.NoError(t, err)
	require.NoError(t, before.Close())

	require.NoError(t, store.EnsureEngine(ctx, database.OwnerURL, db.EnsureEngineParams{Name: name, Password: "second"}))

	_, err = store.Open(ctx, database.EngineURL(t, name, "first"))
	require.Error(t, err, "the old password no longer opens the engine")

	after := openEngine(t, dbtest.Engine{Name: name, URL: database.EngineURL(t, name, "second")})
	kept, err := workflowstore.NewWorkflowStore(after.GetDB()).List(ctx)
	require.NoError(t, err)
	require.Len(t, kept, 1)
	assert.Equal(t, created.ID, kept[0].ID)
}

// TestEnsureRefusesARoleItDidNotCreateHere pins an existing server role never being adopted, logins spanning every database on the server.
func TestEnsureRefusesARoleItDidNotCreateHere(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	first := dbtest.New(t)
	second := dbtest.New(t)
	name := dbtest.EngineName(t, first)
	dbtest.DropEngineWhenDone(t, second, name)

	require.NoError(t, store.EnsureEngine(ctx, first.OwnerURL, db.EnsureEngineParams{Name: name, Password: "first"}))

	err := store.EnsureEngine(ctx, second.OwnerURL, db.EnsureEngineParams{Name: name, Password: "second"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "belongs to no engine")

	_, err = store.Open(ctx, first.EngineURL(t, name, "first"))
	require.NoError(t, err, "the refused attempt left the first database's engine as it was")
}

// TestTheReadOnlyPoolTakesOnlyThisEnginesReader pins the MCP pool refusing every login but the one paired with its engine.
func TestTheReadOnlyPoolTakesOnlyThisEnginesReader(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := dbtest.New(t)
	first := dbtest.NewEngine(t, database)
	second := dbtest.NewEngine(t, database)
	firstID := openEngine(t, first).GetEngine().ID

	tests := []struct {
		name string
		url  string
	}{
		{"another engine's reader", second.ReaderURL},
		{"the engine's own writing login", first.URL},
		{"the database owner", database.OwnerURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := store.OpenReadOnly(ctx, tt.url, firstID)
			require.ErrorIs(t, err, store.ErrNotThisEnginesReader)
		})
	}

	reader, err := store.OpenReadOnly(ctx, first.ReaderURL, firstID)
	require.NoError(t, err)
	reader.Close()
}
