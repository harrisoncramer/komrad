package watch

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"weave/internal/agents"
	"weave/internal/watch"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	markstore "weave/internal/store/mark"
	rowstore "weave/internal/store/row"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStores struct {
	agents    []agents.Row
	workflows []workflowstore.Workflow
	marks     []markstore.Mark
}

func (f *fakeStores) ListRows(_ context.Context, page rowstore.Page) ([]agents.Row, error) {
	return pageOf(f.agents, page.Cursor == nil, func(row agents.Row) bool { return page.Cursor != nil && row.ID == page.Cursor.ID }, int(page.Limit)), nil
}

func (f *fakeStores) ListLiveRowsByID(_ context.Context, ids []string) ([]agents.Row, error) {
	return xslice.Filter(f.agents, func(row agents.Row) bool { return slices.Contains(ids, row.ID) }), nil
}

func (f *fakeStores) ListPage(_ context.Context, limit int64, cursor *workflowstore.Cursor, _ bool) ([]workflowstore.Workflow, error) {
	return pageOf(f.workflows, cursor == nil, func(row workflowstore.Workflow) bool { return cursor != nil && row.ID == cursor.ID }, int(limit)), nil
}

func (f *fakeStores) ListLiveByID(_ context.Context, ids []string) ([]workflowstore.Workflow, error) {
	return xslice.Filter(f.workflows, func(row workflowstore.Workflow) bool { return slices.Contains(ids, row.ID) }), nil
}

func (f *fakeStores) ListMarks(context.Context) ([]markstore.Mark, error) {
	return f.marks, nil
}

// fakeSchedules is the schedule store on its own.
type fakeSchedules struct {
	schedules []schedulestore.Schedule
}

func (f *fakeSchedules) ListPage(_ context.Context, limit int64, cursor *schedulestore.Cursor) ([]schedulestore.Schedule, error) {
	return pageOf(f.schedules, cursor == nil, func(row schedulestore.Schedule) bool { return cursor != nil && row.ID == cursor.ID }, int(limit)), nil
}

func (f *fakeSchedules) ListLiveByID(_ context.Context, ids []string) ([]schedulestore.Schedule, error) {
	return xslice.Filter(f.schedules, func(row schedulestore.Schedule) bool { return slices.Contains(ids, row.ID) }), nil
}

// pageOf is one page of rows after the cursor row, the first page when there is no cursor.
func pageOf[R any](rows []R, first bool, isCursor func(R) bool, limit int) []R {
	start := 0
	if !first {
		for i, row := range rows {
			if isCursor(row) {
				start = i + 1
			}
		}
	}

	page := rows[start:]
	if len(page) > limit {
		page = page[:limit]
	}

	return page
}

func agentRow(id string) agents.Row {
	return agents.Row{Row: rowstore.Row{Agent: agentstore.Agent{ID: id, Name: "agent-" + id}}}
}

func newRows(stores *fakeStores, schedules *fakeSchedules) *Rows {
	return NewRows(NewRowsParams{
		Agents:    stores,
		Workflows: stores,
		Schedules: schedules,
		Marks:     stores,
		Now:       func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
}

// TestReadAnswersARowOrATombstoneForEveryTouchedID pins the read-back: a touched id the
// listing holds comes back as its row and one it does not as a tombstone, in the order asked.
func TestReadAnswersARowOrATombstoneForEveryTouchedID(t *testing.T) {
	mark := "w"
	stores := &fakeStores{
		agents:    []agents.Row{agentRow("a1")},
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
		marks:     []markstore.Mark{{Mark: mark, WorkflowID: &[]string{"w1"}[0]}},
	}
	schedules := &fakeSchedules{schedules: []schedulestore.Schedule{{ID: "s1", Name: "nightly", Cron: "0 2 * * *", Enabled: true}}}

	events, err := newRows(stores, schedules).Read(t.Context(), watch.Touched{
		Agents:    []string{"a1", "a2"},
		Workflows: []string{"w1", "w2"},
		Schedules: []string{"s1", "s2"},
	})
	require.NoError(t, err)
	require.Len(t, events, 6)

	assert.Equal(t, watch.KindAgent, events[0].Kind)
	assert.Equal(t, "agent-a1", events[0].Agent.GetName())
	assert.True(t, events[1].IsTombstone())
	assert.Equal(t, "a2", events[1].ID)

	assert.Equal(t, watch.KindWorkflow, events[2].Kind)
	assert.Equal(t, "backend", events[2].Workflow.GetName())
	assert.Equal(t, "w", events[2].Workflow.GetMark(), "the workflow row carries its mark")
	assert.True(t, events[3].IsTombstone())
	assert.Equal(t, watch.KindWorkflow, events[3].Kind)

	assert.Equal(t, watch.KindSchedule, events[4].Kind)
	assert.Equal(t, "nightly", events[4].Schedule.GetName())
	assert.NotNil(t, events[4].Schedule.GetNextRunAt(), "the next run is worked out from the clock")
	assert.True(t, events[5].IsTombstone())
	assert.Equal(t, watch.KindSchedule, events[5].Kind)
}

// TestReadOfNothingReadsNothing pins that an empty touch costs no query.
func TestReadOfNothingReadsNothing(t *testing.T) {
	events, err := newRows(&fakeStores{}, &fakeSchedules{}).Read(t.Context(), watch.Touched{})
	require.NoError(t, err)
	assert.Empty(t, events)
}

// TestSnapshotWalksEveryListingToTheEnd pins that a snapshot pages each listing past its first
// page and carries every kind of row.
func TestSnapshotWalksEveryListingToTheEnd(t *testing.T) {
	stores := &fakeStores{}
	for i := range snapshotPage + 3 {
		stores.agents = append(stores.agents, agentRow(fmt.Sprintf("a%d", i)))
	}
	for i := range 2 {
		stores.workflows = append(stores.workflows, workflowstore.Workflow{ID: fmt.Sprintf("w%d", i), Name: fmt.Sprintf("flow-%d", i)})
	}
	schedules := &fakeSchedules{schedules: []schedulestore.Schedule{{ID: "s0", Name: "nightly"}}}

	snapshot, err := newRows(stores, schedules).Snapshot(t.Context())
	require.NoError(t, err)

	assert.Len(t, snapshot.Agents, snapshotPage+3)
	assert.Len(t, snapshot.Workflows, 2)
	assert.Len(t, snapshot.Schedules, 1)
}
