package row_test

import (
	"testing"

	"komrad/internal/store"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	agentstore "komrad/internal/store/agent"
	documentstore "komrad/internal/store/document"
	rowstore "komrad/internal/store/row"
	runstore "komrad/internal/store/run"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rowOf reads one agent's row back.
func rowOf(t *testing.T, st *storetest.Stores, id string) rowstore.Row {
	t.Helper()

	row, err := st.Rows.Get(t.Context(), id)
	require.NoError(t, err)

	return *row
}

// TestRowCarriesTheAgent pins that the row is the agent as stored, failure reason included, with
// nothing beside it for an agent nothing has happened to.
func TestRowCarriesTheAgent(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:   "alpha",
		Cwd:    "/repo",
		Prompt: "go",
	})
	_, err := st.Agents.SetFailureReason(ctx, alpha.ID, "worktree is dirty")
	require.NoError(t, err)

	row := rowOf(t, st, alpha.ID)

	assert.Equal(t, "alpha", row.Name)
	assert.Equal(t, "/repo", row.Cwd)
	assert.Equal(t, "go", row.Prompt)
	assert.Equal(t, "worktree is dirty", row.FailureReason)
	assert.False(t, row.FailedRun)
	assert.Nil(t, row.FailedRunLog)
}

// TestFailedRun pins the mark a failed script puts on a row: it names the run that failed last,
// a run that succeeded is nothing to read, and reading clears it.
func TestFailedRun(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})

	fine, err := st.Runs.Begin(ctx, runstore.BeginParams{
		AgentID: alpha.ID,
		Trigger: "post-launch",
		Script:  "setup",
	})
	require.NoError(t, err)
	require.NoError(t, st.Runs.Finish(ctx, runstore.FinishParams{ID: fine, ExitCode: xptr.Ptr(int32(0))}))
	assert.False(t, rowOf(t, st, alpha.ID).FailedRun, "a run that succeeded is nothing to read")

	for number := range 3 {
		id, err := st.Runs.Begin(ctx, runstore.BeginParams{
			AgentID:   alpha.ID,
			Trigger:   "post-launch",
			Script:    "setup",
			LogNumber: xptr.Ptr(int32(number + 1)),
		})
		require.NoError(t, err)
		require.NoError(t, st.Runs.Finish(ctx, runstore.FinishParams{ID: id, Failure: "it failed"}))
	}

	row := rowOf(t, st, alpha.ID)
	assert.True(t, row.FailedRun)
	require.NotNil(t, row.FailedRunLog, "the mark carries the run to open on")
	assert.Equal(t, int32(3), *row.FailedRunLog, "which is the one that failed last")

	require.NoError(t, st.Runs.MarkRead(ctx, alpha.ID))
	assert.False(t, rowOf(t, st, alpha.ID).FailedRun, "reading it clears the mark")
}

// TestDocuments pins the count of an agent's documents and the time of its newest write, which
// a rewrite moves without changing the count.
func TestDocuments(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	author := st.SeedAgent(t, agentstore.Agent{Name: "author"})
	other := st.SeedAgent(t, agentstore.Agent{Name: "other"})

	row := rowOf(t, st, author.ID)
	assert.Zero(t, row.Documents)
	assert.Nil(t, row.DocumentsUpdatedAt, "an agent with no documents has no newest write")

	write := func(name string) *documentstore.Document {
		created, err := st.Documents.Create(ctx, documentstore.CreateParams{AgentID: author.ID, Name: name, Kind: documentstore.KindNotes, Body: name})
		require.NoError(t, err)

		return created
	}
	write("first")
	second := write("second")

	row = rowOf(t, st, author.ID)
	assert.Equal(t, int32(2), row.Documents)
	require.NotNil(t, row.DocumentsUpdatedAt)
	assert.True(t, row.DocumentsUpdatedAt.Equal(second.UpdatedAt), "the newest write is the one that landed last")
	assert.Zero(t, rowOf(t, st, other.ID).Documents, "another agent's documents are not counted")

	rewritten, err := st.Documents.Update(ctx, documentstore.UpdateParams{ID: second.ID, AgentID: author.ID, Name: "second", Kind: documentstore.KindNotes, Body: "again", Version: second.Version})
	require.NoError(t, err)

	row = rowOf(t, st, author.ID)
	assert.Equal(t, int32(2), row.Documents)
	assert.True(t, row.DocumentsUpdatedAt.Equal(rewritten.UpdatedAt), "a rewrite moves the newest write")
}

// TestListPage pins the paging: live or archived rows, newest first, from the cursor on, with
// the rows a delete has claimed left out.
func TestListPage(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	st.SeedAgent(t, agentstore.Agent{Name: "first"})
	second := st.SeedAgent(t, agentstore.Agent{Name: "second"})
	third := st.SeedAgent(t, agentstore.Agent{Name: "third"})
	archived := st.SeedAgent(t, agentstore.Agent{Name: "archived"})
	deleting := st.SeedAgent(t, agentstore.Agent{Name: "deleting"})
	archivedDeleting := st.SeedAgent(t, agentstore.Agent{Name: "archived-deleting"})
	_, err := st.Agents.Archive(ctx, []string{archived.ID, archivedDeleting.ID})
	require.NoError(t, err)
	require.NoError(t, st.Agents.MarkDeleting(ctx, []string{deleting.ID, archivedDeleting.ID}))

	names := func(rows []rowstore.Row) []string {
		listed := xslice.Map(rows, func(row rowstore.Row) string {
			return row.Name
		})

		return listed
	}

	live, err := st.Rows.ListPage(ctx, rowstore.Page{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"third", "second", "first"}, names(live))

	rest, err := st.Rows.ListPage(ctx, rowstore.Page{Limit: 1, Cursor: &agentstore.Cursor{ID: third.ID}})
	require.NoError(t, err)
	assert.Equal(t, []string{"second"}, names(rest))
	assert.Equal(t, second.ID, rest[0].ID)

	old, err := st.Rows.ListPage(ctx, rowstore.Page{Limit: 10, Archived: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"archived"}, names(old))
	assert.Equal(t, archived.ID, old[0].ID)
}

// TestListWatched pins the watched set: the live and archived rows among the given ids answer, one
// a delete has claimed and an id nothing holds are left out, and one that could not be an id at
// all does not fail the read.
func TestListWatched(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	first := st.SeedAgent(t, agentstore.Agent{Name: "first"})
	second := st.SeedAgent(t, agentstore.Agent{Name: "second"})
	archived := st.SeedAgent(t, agentstore.Agent{Name: "archived"})
	deleting := st.SeedAgent(t, agentstore.Agent{Name: "deleting"})
	_, err := st.Agents.Archive(ctx, []string{archived.ID})
	require.NoError(t, err)
	require.NoError(t, st.Agents.MarkDeleting(ctx, []string{deleting.ID}))

	rows, err := st.Rows.ListWatched(ctx, []string{first.ID, second.ID, archived.ID, deleting.ID, "00000000-0000-0000-0000-000000000000", "not-an-id"})
	require.NoError(t, err)
	assert.Equal(t, []string{archived.ID, second.ID, first.ID}, xslice.Map(rows, func(row rowstore.Row) string { return row.ID }))

	rows, err = st.Rows.ListWatched(ctx, []string{"not-an-id"})
	require.NoError(t, err)
	assert.Empty(t, rows)

	live, err := st.Rows.ListWatched(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{archived.ID, second.ID, first.ID}, xslice.Map(live, func(row rowstore.Row) string { return row.ID }), "the whole watched set reads the same way")
}

// TestGet pins the one-row read: an archived row answers, and one a delete has claimed, an id
// nothing holds, or one that could not be an id at all, is not found.
func TestGet(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	archived := st.SeedAgent(t, agentstore.Agent{Name: "archived"})
	deleting := st.SeedAgent(t, agentstore.Agent{Name: "deleting"})
	_, err := st.Agents.Archive(ctx, []string{archived.ID})
	require.NoError(t, err)
	require.NoError(t, st.Agents.MarkDeleting(ctx, []string{deleting.ID}))

	row, err := st.Rows.Get(ctx, archived.ID)
	require.NoError(t, err)
	assert.Equal(t, "archived", row.Name)
	assert.NotNil(t, row.ArchivedAt)

	_, err = st.Rows.Get(ctx, deleting.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = st.Rows.Get(ctx, "00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = st.Rows.Get(ctx, "not-an-id")
	require.ErrorIs(t, err, store.ErrNotFound)
}
