package watch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRow is a row of either resource the tests watch.
type testRow struct {
	ID        string
	Name      string
	UpdatedAt time.Time
}

// fakeStore is one resource's store: every row its watched set holds, and the ones among the
// keys asked.
type fakeStore struct {
	mu   sync.Mutex
	rows []testRow
	// calls is the keys of each read-back, in order.
	calls [][]string
	// held blocks a read-back while it is set, so a test can pile pending keys up behind one render.
	held chan struct{}
	err  error
}

func (f *fakeStore) ListWatched(_ context.Context, keys []string) ([]testRow, error) {
	f.mu.Lock()
	if keys == nil {
		defer f.mu.Unlock()

		return f.rows, nil
	}

	f.calls = append(f.calls, keys)
	held, err := f.held, f.err
	rows := slices.Clone(f.rows)
	f.mu.Unlock()

	if held != nil {
		<-held
	}
	if err != nil {
		return nil, err
	}

	return xslice.Filter(rows, func(row testRow) bool { return slices.Contains(keys, row.ID) }), nil
}

// reads is the keys of every read-back so far.
func (f *fakeStore) reads() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.calls)
}

// put writes a row the next read-back answers with.
func (f *fakeStore) put(rows ...testRow) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.rows = rows
}

// fakeStores is every resource's store.
type fakeStores struct {
	agents    *fakeStore
	workflows *fakeStore
}

// TestReadAnswersARowOrATombstoneForEveryPendingKey pins the read-back: a pending key the
// watched set holds comes back as its row and one it does not as a tombstone, kind by kind
// and in the order asked.
func TestReadAnswersARowOrATombstoneForEveryPendingKey(t *testing.T) {
	stores := newStores()
	stores.agents.put(agentRow("a1"))
	stores.workflows.put(testRow{ID: "w1", Name: "backend"})

	changes, err := newRows(stores).Read(t.Context(), pendingKeys{
		watchv1.Resource_RESOURCE_WORKFLOW: []string{"w1", "w2"},
		watchv1.Resource_RESOURCE_AGENT:    []string{"a1", "a2"},
	})
	require.NoError(t, err)
	require.Len(t, changes, 4)

	keys := xslice.Map(changes, func(change Change) string { return change.Key })
	assert.Equal(t, []string{"a1", "a2", "w1", "w2"}, keys)
	for i := 1; i < len(changes); i += 2 {
		assert.Nil(t, changes[i].Row, "%s is a tombstone", changes[i].Key)
		assert.Equal(t, changes[i-1].Resource, changes[i].Resource)
	}

	assert.Equal(t, "agent-a1", changes[0].Row.GetAgent().GetName())
	assert.Equal(t, "backend", changes[2].Row.GetWorkflow().GetName())
}

// TestReadOfNothingReadsNothing pins that an empty pending set costs no query.
func TestReadOfNothingReadsNothing(t *testing.T) {
	stores := newStores()

	changes, err := newRows(stores).Read(t.Context(), pendingKeys{})
	require.NoError(t, err)
	assert.Empty(t, changes)
	assert.Empty(t, stores.agents.reads())
}

// TestSnapshotCarriesEveryWatchedSet pins that a snapshot carries every row of every kind, one
// resource after another.
func TestSnapshotCarriesEveryWatchedSet(t *testing.T) {
	stores := newStores()
	stores.agents.put(agentRow("a0"), agentRow("a1"), agentRow("a2"))
	stores.workflows.put(testRow{ID: "w0", Name: "flow-0"}, testRow{ID: "w1", Name: "flow-1"})

	snapshot, err := newRows(stores).Snapshot(t.Context())
	require.NoError(t, err)

	kinds := xslice.Map(snapshot, func(change Change) string { return fmt.Sprintf("%T", change.Row.GetRow()) })
	assert.Equal(t, []string{
		"*watchv1.Row_Agent", "*watchv1.Row_Agent", "*watchv1.Row_Agent",
		"*watchv1.Row_Workflow", "*watchv1.Row_Workflow",
	}, kinds)

	keys := xslice.Map(snapshot, func(change Change) string { return change.Key })
	assert.Equal(t, []string{"a0", "a1", "a2", "w0", "w1"}, keys)
	assert.Equal(t, watchv1.Resource_RESOURCE_WORKFLOW, snapshot[3].Resource, "a snapshot row says which resource it is")
}

// TestAFailedSnapshotNamesItsResource pins that a lister's error comes back wrapped with the resource it was reading.
func TestAFailedSnapshotNamesItsResource(t *testing.T) {
	boom := errors.New("boom")
	rows := NewRows([]Loader{
		NewLoader(watchv1.Resource_RESOURCE_MARK, failingLister{err: boom}, renderTestAgent),
	})

	_, err := rows.Snapshot(t.Context())
	require.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), watchv1.Resource_RESOURCE_MARK.String())
}

// failingLister answers every read with its error.
type failingLister struct {
	err error
}

func (f failingLister) ListWatched(context.Context, []string) ([]testRow, error) {
	return nil, f.err
}

func agentRow(id string) testRow {
	return testRow{ID: id, Name: "agent-" + id}
}

func newStores() *fakeStores {
	return &fakeStores{
		agents:    &fakeStore{},
		workflows: &fakeStore{},
	}
}

func newRows(stores *fakeStores) *Rows {
	return NewRows([]Loader{
		NewLoader(watchv1.Resource_RESOURCE_AGENT, stores.agents, renderTestAgent),
		NewLoader(watchv1.Resource_RESOURCE_WORKFLOW, stores.workflows, renderTestWorkflow),
	})
}

func renderTestAgent(row testRow) (string, *watchv1.Row) {
	return row.ID, &watchv1.Row{Row: &watchv1.Row_Agent{Agent: &agentv1.Agent{
		Id:   row.ID,
		Name: row.Name,
	}}}
}

func renderTestWorkflow(row testRow) (string, *watchv1.Row) {
	return row.ID, &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: &workflowv1.Workflow{
		Id:   row.ID,
		Name: row.Name,
	}}}
}
