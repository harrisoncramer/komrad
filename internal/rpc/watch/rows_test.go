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

type fakeStores struct {
	agents       []rowstore.Row
	workflows    []workflowstore.Workflow
	marks        []markstore.Mark
	mail         []mailstore.Mail
	pullRequests []pullrequeststore.PullRequest
}

func (f *fakeStores) ListRows(_ context.Context, page rowstore.Page) ([]rowstore.Row, error) {
	return pageOf(f.agents, page.Cursor == nil, func(row rowstore.Row) bool { return page.Cursor != nil && row.ID == page.Cursor.ID }, int(page.Limit)), nil
}

func (f *fakeStores) ListLiveRowsByID(_ context.Context, ids []string) ([]rowstore.Row, error) {
	return xslice.Filter(f.agents, func(row rowstore.Row) bool { return slices.Contains(ids, row.ID) }), nil
}

func (f *fakeStores) ListPage(_ context.Context, limit int64, cursor *workflowstore.Cursor, _ bool) ([]workflowstore.Workflow, error) {
	return pageOf(f.workflows, cursor == nil, func(row workflowstore.Workflow) bool { return cursor != nil && row.ID == cursor.ID }, int(limit)), nil
}

func (f *fakeStores) ListLiveByID(_ context.Context, ids []string) ([]workflowstore.Workflow, error) {
	return xslice.Filter(f.workflows, func(row workflowstore.Workflow) bool { return slices.Contains(ids, row.ID) }), nil
}

func (f *fakeStores) List(context.Context) ([]markstore.Mark, error) {
	return f.marks, nil
}

func (f *fakeStores) ListByKey(_ context.Context, marks []string) ([]markstore.Mark, error) {
	return xslice.Filter(f.marks, func(row markstore.Mark) bool { return slices.Contains(marks, row.Mark) }), nil
}

// fakeMail is the mail store on its own.
type fakeMail struct {
	mail []mailstore.Mail
}

func (f *fakeMail) ListUnclaimed(context.Context) ([]mailstore.Mail, error) {
	return f.mail, nil
}

func (f *fakeMail) ListByKey(_ context.Context, ids []string) ([]mailstore.Mail, error) {
	return xslice.Filter(f.mail, func(row mailstore.Mail) bool { return slices.Contains(ids, row.ID) }), nil
}

// fakePullRequests is the pull request store on its own.
type fakePullRequests struct {
	pullRequests []pullrequeststore.PullRequest
}

func (f *fakePullRequests) ListOnLiveBranches(context.Context) ([]pullrequeststore.PullRequest, error) {
	return f.pullRequests, nil
}

func (f *fakePullRequests) ListByKey(_ context.Context, ids []string) ([]pullrequeststore.PullRequest, error) {
	return xslice.Filter(f.pullRequests, func(row pullrequeststore.PullRequest) bool { return slices.Contains(ids, row.ID) }), nil
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

func agentRow(id string) rowstore.Row {
	return rowstore.Row{Agent: agentstore.Agent{ID: id, Name: "agent-" + id}}
}

func newRows(stores *fakeStores, schedules *fakeSchedules) *Rows {
	return NewRows(NewRowsParams{
		Agents:       stores,
		Workflows:    stores,
		Schedules:    schedules,
		Marks:        stores,
		Mail:         &fakeMail{mail: stores.mail},
		PullRequests: &fakePullRequests{pullRequests: stores.pullRequests},
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
	}
	schedules := &fakeSchedules{schedules: []schedulestore.Schedule{{ID: "s1", Name: "nightly", Cron: "0 2 * * *", Enabled: true}}}

	events, err := newRows(stores, schedules).Read(t.Context(), watch.Touched{
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
	events, err := newRows(&fakeStores{}, &fakeSchedules{}).Read(t.Context(), watch.Touched{})
	require.NoError(t, err)
	assert.Empty(t, events)
}

// TestSnapshotWalksEveryWatchedSetToTheEnd pins that a snapshot pages each listing past its
// first page and carries every kind of row, one resource after another.
func TestSnapshotWalksEveryWatchedSetToTheEnd(t *testing.T) {
	stores := &fakeStores{
		marks:        []markstore.Mark{{Mark: "a"}},
		mail:         []mailstore.Mail{{ID: "m0"}},
		pullRequests: []pullrequeststore.PullRequest{{ID: "p0"}},
	}
	for i := range snapshotPage + 3 {
		stores.agents = append(stores.agents, agentRow(fmt.Sprintf("a%d", i)))
	}
	for i := range 2 {
		stores.workflows = append(stores.workflows, workflowstore.Workflow{ID: fmt.Sprintf("w%d", i), Name: fmt.Sprintf("flow-%d", i)})
	}
	schedules := &fakeSchedules{schedules: []schedulestore.Schedule{{ID: "s0", Name: "nightly"}}}

	snapshot, err := newRows(stores, schedules).Snapshot(t.Context())
	require.NoError(t, err)
	require.Len(t, snapshot, snapshotPage+3+2+1+1+1+1)

	kinds := xslice.Map(snapshot, func(row *watchv1.Row) string { return fmt.Sprintf("%T", row.GetRow()) })
	assert.Equal(t, snapshotPage+3, xslice.Count(kinds, func(kind string) bool { return kind == "*watchv1.Row_Agent" }))
	assert.Equal(t, []string{"*watchv1.Row_Workflow", "*watchv1.Row_Workflow", "*watchv1.Row_Schedule", "*watchv1.Row_Mark", "*watchv1.Row_Mail", "*watchv1.Row_PullRequest"}, kinds[snapshotPage+3:])
}

// TestArmsAreTheResources pins the read-back to the resource option: one arm per annotated
// table, and nothing else.
func TestArmsAreTheResources(t *testing.T) {
	rows := newRows(&fakeStores{}, &fakeSchedules{})

	want := xslice.Map(resource.All(), func(r resource.Resource) watch.Kind {
		return watch.Kind(watchv1.Resource_value[r.EnumValue()])
	})
	assert.ElementsMatch(t, want, slices.Collect(maps.Keys(rows.arms)))
}
