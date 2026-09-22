package watch

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"weave/internal/resource"
	"weave/internal/watch"
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
)

// fakeStore is one resource's store: every row its watched set holds, and the ones among the
// keys asked.
type fakeStore[R any] struct {
	rows []R
	key  func(R) string
}

func (f *fakeStore[R]) List(context.Context) ([]R, error) {
	return f.rows, nil
}

func (f *fakeStore[R]) ListByKey(_ context.Context, keys []string) ([]R, error) {
	return xslice.Filter(f.rows, func(row R) bool { return slices.Contains(keys, f.key(row)) }), nil
}

// fakeStores is every resource's store.
type fakeStores struct {
	agents       []rowstore.Row
	workflows    []workflowstore.Workflow
	schedules    []schedulestore.Schedule
	marks        []markstore.Mark
	mail         []mailstore.Mail
	pullRequests []pullrequeststore.PullRequest
}

func agentRow(id string) rowstore.Row {
	return rowstore.Row{Agent: agentstore.Agent{ID: id, Name: "agent-" + id}}
}

func newRows(stores *fakeStores) *Rows {
	return NewRows(NewRowsParams{
		Agents:       &fakeStore[rowstore.Row]{rows: stores.agents, key: func(row rowstore.Row) string { return row.ID }},
		Workflows:    &fakeStore[workflowstore.Workflow]{rows: stores.workflows, key: func(row workflowstore.Workflow) string { return row.ID }},
		Schedules:    &fakeStore[schedulestore.Schedule]{rows: stores.schedules, key: func(row schedulestore.Schedule) string { return row.ID }},
		Marks:        &fakeStore[markstore.Mark]{rows: stores.marks, key: func(row markstore.Mark) string { return row.Mark }},
		Mail:         &fakeStore[mailstore.Mail]{rows: stores.mail, key: func(row mailstore.Mail) string { return row.ID }},
		PullRequests: &fakeStore[pullrequeststore.PullRequest]{rows: stores.pullRequests, key: func(row pullrequeststore.PullRequest) string { return row.ID }},
		Now:          func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
}

// TestReadAnswersARowOrATombstoneForEveryTouchedKey pins the read-back: a touched key the
// watched set holds comes back as its row and one it does not as a tombstone, kind by kind
// and in the order asked.
func TestReadAnswersARowOrATombstoneForEveryTouchedKey(t *testing.T) {
	stores := &fakeStores{
		agents:       []rowstore.Row{agentRow("a1")},
		workflows:    []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
		marks:        []markstore.Mark{{Mark: "w", WorkflowID: &[]string{"w1"}[0]}},
		mail:         []mailstore.Mail{{ID: "m1", ToAgentID: "a1"}},
		pullRequests: []pullrequeststore.PullRequest{{ID: "p1", Number: 7}},
		schedules:    []schedulestore.Schedule{{ID: "s1", Name: "nightly", Cron: "0 2 * * *", Enabled: true}},
	}

	events, err := newRows(stores).Read(t.Context(), watch.Touched{
		watch.KindAgent:       []string{"a1", "a2"},
		watch.KindWorkflow:    []string{"w1", "w2"},
		watch.KindSchedule:    []string{"s1", "s2"},
		watch.KindMark:        []string{"w", "x"},
		watch.KindMail:        []string{"m1", "m2"},
		watch.KindPullRequest: []string{"p1", "p2"},
	})
	require.NoError(t, err)
	require.Len(t, events, 12)

	keys := xslice.Map(events, func(event watch.Event) string { return event.Key })
	assert.Equal(t, []string{"a1", "a2", "w1", "w2", "s1", "s2", "w", "x", "m1", "m2", "p1", "p2"}, keys)
	for i := 1; i < len(events); i += 2 {
		assert.Nil(t, events[i].Row, "%s is a tombstone", events[i].Key)
		assert.Equal(t, events[i-1].Kind, events[i].Kind)
	}

	assert.Equal(t, "agent-a1", events[0].Row.GetAgent().GetName())
	assert.Equal(t, "backend", events[2].Row.GetWorkflow().GetName())
	assert.Equal(t, "nightly", events[4].Row.GetSchedule().GetName())
	assert.NotNil(t, events[4].Row.GetSchedule().GetNextRunAt(), "the next run is worked out from the clock")
	assert.Equal(t, "w1", events[6].Row.GetMark().GetWorkflowId())
	assert.Equal(t, "a1", events[8].Row.GetMail().GetToAgentId())
	assert.Equal(t, int32(7), events[10].Row.GetPullRequest().GetNumber())
}

// TestReadOfNothingReadsNothing pins that an empty touch costs no query.
func TestReadOfNothingReadsNothing(t *testing.T) {
	events, err := newRows(&fakeStores{}).Read(t.Context(), watch.Touched{})
	require.NoError(t, err)
	assert.Empty(t, events)
}

// TestSnapshotCarriesEveryWatchedSet pins that a snapshot carries every row of every kind, one
// resource after another.
func TestSnapshotCarriesEveryWatchedSet(t *testing.T) {
	stores := &fakeStores{
		agents:       []rowstore.Row{agentRow("a0"), agentRow("a1"), agentRow("a2")},
		workflows:    []workflowstore.Workflow{{ID: "w0", Name: "flow-0"}, {ID: "w1", Name: "flow-1"}},
		schedules:    []schedulestore.Schedule{{ID: "s0", Name: "nightly"}},
		marks:        []markstore.Mark{{Mark: "a"}},
		mail:         []mailstore.Mail{{ID: "m0"}},
		pullRequests: []pullrequeststore.PullRequest{{ID: "p0"}},
	}

	snapshot, err := newRows(stores).Snapshot(t.Context())
	require.NoError(t, err)

	kinds := xslice.Map(snapshot, func(row *watchv1.Row) string { return fmt.Sprintf("%T", row.GetRow()) })
	assert.Equal(t, []string{
		"*watchv1.Row_Agent", "*watchv1.Row_Agent", "*watchv1.Row_Agent",
		"*watchv1.Row_Workflow", "*watchv1.Row_Workflow",
		"*watchv1.Row_Schedule",
		"*watchv1.Row_Mark",
		"*watchv1.Row_Mail",
		"*watchv1.Row_PullRequest",
	}, kinds)
}

// TestArmsAreTheResources pins the read-back to the resource option: one arm per annotated
// table, and nothing else.
func TestArmsAreTheResources(t *testing.T) {
	rows := newRows(&fakeStores{})

	want := xslice.Map(resource.All(), func(r resource.Resource) watch.Kind {
		return watch.Kind(watchv1.Resource_value[r.EnumValue()])
	})
	assert.ElementsMatch(t, want, slices.Collect(maps.Keys(rows.arms)))
}
