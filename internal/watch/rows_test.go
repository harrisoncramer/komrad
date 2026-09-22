package watch

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"weave/lib/xptr"
	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	rowstore "weave/internal/store/row"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// fakeStore is one resource's store: every row its watched set holds, and the ones among the
// keys asked.
type fakeStore[R any] struct {
	mu   sync.Mutex
	rows []R
	key  func(R) string
	// calls is the keys of each read-back, in order.
	calls [][]string
	// held blocks a read-back while it is set, so a test can pile touches up behind one render.
	held chan struct{}
	err  error
}

func (f *fakeStore[R]) List(context.Context) ([]R, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.rows, nil
}

func (f *fakeStore[R]) ListByKey(_ context.Context, keys []string) ([]R, error) {
	f.mu.Lock()
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

	return xslice.Filter(rows, func(row R) bool { return slices.Contains(keys, f.key(row)) }), nil
}

// reads is the keys of every read-back so far.
func (f *fakeStore[R]) reads() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.calls)
}

// put writes a row the next read-back answers with.
func (f *fakeStore[R]) put(rows ...R) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.rows = rows
}

// fakeStores is every resource's store.
type fakeStores struct {
	agents       *fakeStore[rowstore.Row]
	workflows    *fakeStore[workflowstore.Workflow]
	schedules    *fakeStore[schedulestore.Schedule]
	marks        *fakeStore[markstore.Mark]
	mail         *fakeStore[mailstore.Mail]
	pullRequests *fakeStore[pullrequeststore.PullRequest]
}

func agentRow(id string) rowstore.Row {
	return rowstore.Row{Agent: agentstore.Agent{ID: id, Name: "agent-" + id}}
}

func newStores() *fakeStores {
	return &fakeStores{
		agents:       &fakeStore[rowstore.Row]{key: func(row rowstore.Row) string { return row.ID }},
		workflows:    &fakeStore[workflowstore.Workflow]{key: func(row workflowstore.Workflow) string { return row.ID }},
		schedules:    &fakeStore[schedulestore.Schedule]{key: func(row schedulestore.Schedule) string { return row.ID }},
		marks:        &fakeStore[markstore.Mark]{key: func(row markstore.Mark) string { return row.Mark }},
		mail:         &fakeStore[mailstore.Mail]{key: func(row mailstore.Mail) string { return row.ID }},
		pullRequests: &fakeStore[pullrequeststore.PullRequest]{key: func(row pullrequeststore.PullRequest) string { return row.ID }},
	}
}

func storesFor(stores *fakeStores) Stores {
	return Stores{
		Agents:       stores.agents,
		Workflows:    stores.workflows,
		Schedules:    stores.schedules,
		Marks:        stores.marks,
		Mail:         stores.mail,
		PullRequests: stores.pullRequests,
	}
}

// fixedClock is the clock the rendered rows are drawn against.
func fixedClock() time.Time {
	return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
}

func newRows(stores *fakeStores) *Rows {
	return NewRows(NewRowsParams{Stores: storesFor(stores), Now: fixedClock})
}

// TestReadAnswersARowOrATombstoneForEveryTouchedKey pins the read-back: a touched key the
// watched set holds comes back as its row and one it does not as a tombstone, kind by kind
// and in the order asked.
func TestReadAnswersARowOrATombstoneForEveryTouchedKey(t *testing.T) {
	stores := newStores()
	stores.agents.put(agentRow("a1"))
	stores.workflows.put(workflowstore.Workflow{ID: "w1", Name: "backend"})
	stores.marks.put(markstore.Mark{Mark: "w", WorkflowID: xptr.Ptr("w1")})
	stores.mail.put(mailstore.Mail{ID: "m1", ToAgentID: "a1"})
	stores.pullRequests.put(pullrequeststore.PullRequest{ID: "p1", Number: 7})
	stores.schedules.put(schedulestore.Schedule{ID: "s1", Name: "nightly", Cron: "0 2 * * *", Enabled: true})

	changes, err := newRows(stores).Read(t.Context(), Touched{
		watchv1.Resource_RESOURCE_AGENT:        []string{"a1", "a2"},
		watchv1.Resource_RESOURCE_WORKFLOW:     []string{"w1", "w2"},
		watchv1.Resource_RESOURCE_SCHEDULE:     []string{"s1", "s2"},
		watchv1.Resource_RESOURCE_MARK:         []string{"w", "x"},
		watchv1.Resource_RESOURCE_MAIL:         []string{"m1", "m2"},
		watchv1.Resource_RESOURCE_PULL_REQUEST: []string{"p1", "p2"},
	})
	require.NoError(t, err)
	require.Len(t, changes, 12)

	keys := xslice.Map(changes, func(change Change) string { return change.Key })
	assert.Equal(t, []string{"a1", "a2", "w1", "w2", "s1", "s2", "w", "x", "m1", "m2", "p1", "p2"}, keys)
	for i := 1; i < len(changes); i += 2 {
		assert.Nil(t, changes[i].Row, "%s is a tombstone", changes[i].Key)
		assert.Equal(t, changes[i-1].Resource, changes[i].Resource)
	}

	assert.Equal(t, "agent-a1", changes[0].Row.GetAgent().GetName())
	assert.Equal(t, "backend", changes[2].Row.GetWorkflow().GetName())
	assert.Equal(t, "nightly", changes[4].Row.GetSchedule().GetName())
	assert.NotNil(t, changes[4].Row.GetSchedule().GetNextRunAt(), "the next run is worked out from the clock")
	assert.Equal(t, "w1", changes[6].Row.GetMark().GetWorkflowId())
	assert.Equal(t, "a1", changes[8].Row.GetMail().GetToAgentId())
	assert.Equal(t, int32(7), changes[10].Row.GetPullRequest().GetNumber())
}

// TestReadOfNothingReadsNothing pins that an empty touch costs no query.
func TestReadOfNothingReadsNothing(t *testing.T) {
	changes, err := newRows(newStores()).Read(t.Context(), Touched{})
	require.NoError(t, err)
	assert.Empty(t, changes)
}

// TestSnapshotCarriesEveryWatchedSet pins that a snapshot carries every row of every kind, one
// resource after another.
func TestSnapshotCarriesEveryWatchedSet(t *testing.T) {
	stores := newStores()
	stores.agents.put(agentRow("a0"), agentRow("a1"), agentRow("a2"))
	stores.workflows.put(workflowstore.Workflow{ID: "w0", Name: "flow-0"}, workflowstore.Workflow{ID: "w1", Name: "flow-1"})
	stores.schedules.put(schedulestore.Schedule{ID: "s0", Name: "nightly"})
	stores.marks.put(markstore.Mark{Mark: "a"})
	stores.mail.put(mailstore.Mail{ID: "m0"})
	stores.pullRequests.put(pullrequeststore.PullRequest{ID: "p0"})

	snapshot, err := newRows(stores).Snapshot(t.Context())
	require.NoError(t, err)

	kinds := xslice.Map(snapshot, func(change Change) string { return fmt.Sprintf("%T", change.Row.GetRow()) })
	assert.Equal(t, []string{
		"*watchv1.Row_Agent", "*watchv1.Row_Agent", "*watchv1.Row_Agent",
		"*watchv1.Row_Workflow", "*watchv1.Row_Workflow",
		"*watchv1.Row_Schedule",
		"*watchv1.Row_Mark",
		"*watchv1.Row_Mail",
		"*watchv1.Row_PullRequest",
	}, kinds)

	keys := xslice.Map(snapshot, func(change Change) string { return change.Key })
	assert.Equal(t, []string{"a0", "a1", "a2", "w0", "w1", "s0", "a", "m0", "p0"}, keys)
	assert.Equal(t, watchv1.Resource_RESOURCE_MARK, snapshot[6].Resource, "a snapshot row says which resource it is")
}

// TestEveryResourceIsDeclaredWhole pins what an entry of the list has to carry: its table, its
// key, its enum and its message, and a store, a row key, a renderer and a Row arm that draw one
// row from end to end.
func TestEveryResourceIsDeclaredWhole(t *testing.T) {
	stores := newStores()
	stores.agents.put(agentRow("k"))
	stores.workflows.put(workflowstore.Workflow{ID: "k", Name: "flow"})
	stores.schedules.put(schedulestore.Schedule{ID: "k", Name: "nightly"})
	stores.marks.put(markstore.Mark{Mark: "k"})
	stores.mail.put(mailstore.Mail{ID: "k"})
	stores.pullRequests.put(pullrequeststore.PullRequest{ID: "k"})

	declared := getResources(storesFor(stores), fixedClock)
	require.NotEmpty(t, declared)

	for _, r := range declared {
		t.Run(r.Table, func(t *testing.T) {
			assert.NotEmpty(t, r.Key, "no key column")
			assert.NotEqual(t, watchv1.Resource_RESOURCE_UNSPECIFIED, r.Enum, "no enum value")
			require.NotNil(t, r.Message, "no message")
			assert.NotNil(t, r.Message.Fields().ByName(protoreflect.Name(r.Key)), "the message has no %s field", r.Key)

			snapshot, err := drawn(t, r, func() ([]Change, error) { return r.arm.snapshot(t.Context()) })
			require.NoError(t, err)
			require.Len(t, snapshot, 1)
			assert.Equal(t, "k", snapshot[0].Key, "no row key")
			assert.NotNil(t, snapshot[0].Row.GetRow(), "no renderer, or no Row arm to wrap it in")

			held, err := drawn(t, r, func() (map[string]*watchv1.Row, error) { return r.arm.read(t.Context(), []string{"k"}) })
			require.NoError(t, err)
			assert.NotNil(t, held["k"], "the read-back answers no row for the key")
		})
	}
}

// drawn runs one of a resource's arms, failing the test by table when the entry left a piece out.
func drawn[T any](t *testing.T, r Resource, run func() (T, error)) (T, error) {
	t.Helper()

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s declares no store, row key, renderer or Row arm: %v", r.Table, recovered)
		}
	}()

	return run()
}

// TestArmsAreTheResources pins the read-back to the resource list: one arm per resource, and
// nothing else.
func TestArmsAreTheResources(t *testing.T) {
	rows := newRows(newStores())

	want := xslice.Map(Resources(), func(r Resource) watchv1.Resource { return r.Enum })
	assert.ElementsMatch(t, want, slices.Collect(maps.Keys(rows.arms)))
}
