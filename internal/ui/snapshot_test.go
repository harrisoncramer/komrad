package ui

import (
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/view"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshotOne reads one agent row and hands back the single row it made.
func snapshotOne(t *testing.T, item agentItem) view.Row {
	t.Helper()

	return snapshotRowOf(t, snapshotParams{rows: []agentRow{{item: item}}})
}

// snapshotRunning reads one row whose agent is running and doing nothing, which is the row every
// mark below is hung on.
func snapshotRunning(t *testing.T, item agentItem) view.Row {
	t.Helper()

	item.alive = true
	item.agent.State = agentstore.StateIdle

	return snapshotOne(t, item)
}

// snapshotRowOf reads a whole listing and hands back the single row it made.
func snapshotRowOf(t *testing.T, params snapshotParams) view.Row {
	t.Helper()

	snapshot := agentsSnapshot(params)
	require.Len(t, snapshot.Rows, 1)

	return snapshot.Rows[0]
}

// TestSnapshotStatus pins the bar every state of an agent falls to, all seven of them, a failed
// create reading as broken rather than as the idle it would otherwise fall to.
func TestSnapshotStatus(t *testing.T) {
	tests := []struct {
		name string
		item agentItem
		want view.Status
	}{
		{
			name: "a create that failed is broken",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateFailedToCreate}},
			want: view.StatusBroken,
		},
		{
			name: "a teardown that failed is broken",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateFailedToCleanup}},
			want: view.StatusBroken,
		},
		{
			name: "an agent stopped at a prompt wants you",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateWaiting}, alive: true},
			want: view.StatusWaiting,
		},
		{
			name: "a pane nobody can read wants you, since nobody else will look",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateUncertain}, alive: true},
			want: view.StatusWaiting,
		},
		{
			name: "an agent somebody paused wants that person back, whatever it was doing",
			item: agentItem{
				agent: agentstore.Agent{State: agentstore.StateThinking, HeldAt: xptr.Ptr(time.Now())},
				alive: true,
			},
			want: view.StatusWaiting,
		},
		{
			name: "an agent whose pane has gone is idle whatever it stopped in",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateThinking}},
			want: view.StatusIdle,
		},
		{
			name: "an agent still being set up is under way, with nobody blocked",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateSettingUp}},
			want: view.StatusThinking,
		},
		{
			name: "a running agent is thinking",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateThinking}, alive: true},
			want: view.StatusThinking,
		},
		{
			name: "an agent doing nothing is idle",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateIdle}, alive: true},
			want: view.StatusIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, snapshotOne(t, tt.item).Status)
		})
	}
}

// TestSnapshotRepo pins where an agent's own checkout stands, including the create that never
// made one, whose repo column is what tells it from a directory that has since gone.
func TestSnapshotRepo(t *testing.T) {
	tests := []struct {
		name  string
		agent agentstore.Agent
		want  view.Repo
	}{
		{
			name:  "a directory that has gone beats a rebase left in it",
			agent: agentstore.Agent{WorktreeRemoved: true, SequenceInProgress: true},
			want:  view.RepoGone,
		},
		{
			name:  "a create that failed with a directory that has gone still says so",
			agent: agentstore.Agent{State: agentstore.StateFailedToCreate, WorktreeRemoved: true},
			want:  view.RepoGone,
		},
		{
			name:  "a create that failed before there was a directory reads as clean",
			agent: agentstore.Agent{State: agentstore.StateFailedToCreate},
			want:  view.RepoClean,
		},
		{
			name:  "a rebase left part way through says so",
			agent: agentstore.Agent{SequenceInProgress: true},
			want:  view.RepoRebasing,
		},
		{
			name:  "an untouched checkout is clean",
			agent: agentstore.Agent{},
			want:  view.RepoClean,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, snapshotOne(t, agentItem{agent: tt.agent}).Repo)
		})
	}
}

// TestSnapshotReview pins where the local review stands, a released verdict beating the reviewer
// still reading since one column carries both.
func TestSnapshotReview(t *testing.T) {
	tests := []struct {
		name string
		item agentItem
		want view.Review
	}{
		{
			name: "changes requested beats the reviewer still being owed an answer",
			item: agentItem{
				localVerdict:  string(reviewstore.VerdictChangesRequested),
				inLocalReview: true,
			},
			want: view.ReviewChangesRequested,
		},
		{
			name: "an approval beats the reviewer still being owed an answer",
			item: agentItem{
				localVerdict:  string(reviewstore.VerdictApproved),
				inLocalReview: true,
			},
			want: view.ReviewApproved,
		},
		{
			name: "notes without a conclusion leave the review open",
			item: agentItem{
				localVerdict:  string(reviewstore.VerdictCommented),
				inLocalReview: true,
			},
			want: view.ReviewOpen,
		},
		{
			name: "notes on work nobody is still reading leave the column empty",
			item: agentItem{localVerdict: string(reviewstore.VerdictCommented)},
			want: view.ReviewNone,
		},
		{
			name: "a reviewer owed an answer is an open review",
			item: agentItem{inLocalReview: true},
			want: view.ReviewOpen,
		},
		{
			name: "work no reviewer has read has no review",
			item: agentItem{},
			want: view.ReviewNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, snapshotOne(t, tt.item).Review)
		})
	}
}

// TestSnapshotForgeIsNotRead pins that nothing fills the pull request column, since nothing
// weave stores knows about one yet.
func TestSnapshotForgeIsNotRead(t *testing.T) {
	item := agentItem{agent: agentstore.Agent{
		ID:       "a1",
		Metadata: []agentstore.Metadata{{Key: "pr", Value: "https://example.test/1"}},
	}}

	assert.Equal(t, view.ForgeNone, snapshotOne(t, item).Forge)
}

// TestSnapshotMarksOnARow pins the marks a row carries beside its name.
func TestSnapshotMarksOnARow(t *testing.T) {
	tests := []struct {
		name string
		item agentItem
		want view.Row
	}{
		{
			name: "an agent somebody is holding says it is paused",
			item: agentItem{agent: agentstore.Agent{
				ID:     "a1",
				Name:   "held",
				HeldAt: xptr.Ptr(time.Now()),
			}},
			want: view.Row{
				Key:    "agent:a1",
				Name:   "held",
				Held:   true,
				Status: view.StatusWaiting,
			},
		},
		{
			name: "a failed run nobody has read is an alert",
			item: agentItem{
				agent:     agentstore.Agent{ID: "a1", Name: "alert"},
				failedRun: true,
			},
			want: view.Row{
				Key:       "agent:a1",
				Name:      "alert",
				FailedRun: true,
			},
		},
		{
			name: "mail it has not read says so",
			item: agentItem{
				agent:      agentstore.Agent{ID: "a1", Name: "mail"},
				unreadMail: true,
			},
			want: view.Row{
				Key:        "agent:a1",
				Name:       "mail",
				UnreadMail: true,
			},
		},
		{
			name: "an agent a schedule launched says so",
			item: agentItem{agent: agentstore.Agent{
				ID:         "a1",
				Name:       "nightly",
				ScheduleID: xptr.Ptr("s1"),
			}},
			want: view.Row{
				Key:       "agent:a1",
				Name:      "nightly",
				Scheduled: true,
			},
		},
		{
			name: "a hidden agent is greyed rather than given a glyph of its own",
			item: agentItem{agent: agentstore.Agent{
				ID:     "a1",
				Name:   "hidden",
				Hidden: true,
			}},
			want: view.Row{
				Key:  "agent:a1",
				Name: "hidden",
				Dim:  true,
			},
		},
		{
			name: "output nobody has read brightens the name rather than marking it",
			item: agentItem{
				agent:    agentstore.Agent{ID: "a1", Name: "unread"},
				unviewed: true,
			},
			want: view.Row{
				Key:      "agent:a1",
				Name:     "unread",
				Unviewed: true,
			},
		},
		{
			name: "an ordinary agent carries nothing at all",
			item: agentItem{agent: agentstore.Agent{ID: "a1", Name: "ordinary"}},
			want: view.Row{
				Key:  "agent:a1",
				Name: "ordinary",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, snapshotRunning(t, tt.item))
		})
	}
}

// TestSnapshotDormant pins the difference between an agent sitting quiet and one with no process
// behind it at all, which is the one thing the state it wears cannot say.
func TestSnapshotDormant(t *testing.T) {
	tests := []struct {
		name string
		item agentItem
		want bool
	}{
		{
			name: "an agent whose pane has gone has nothing behind it",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateIdle}},
			want: true,
		},
		{
			name: "an agent that stopped mid-thought has nothing behind it either",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateThinking}},
			want: true,
		},
		{
			name: "a create that failed is not dormant, since the row still has a failure to report",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateFailedToCreate}},
			want: false,
		},
		{
			name: "an agent still being set up is not dormant, since it is on its way",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateSettingUp}},
			want: false,
		},
		{
			name: "an agent sitting quiet with a pane of its own is not dormant",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateIdle}, alive: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, snapshotOne(t, tt.item).Dormant)
		})
	}
}

// TestSnapshotKinds pins what each row of the tree stands for.
func TestSnapshotKinds(t *testing.T) {
	tests := []struct {
		name string
		row  agentRow
		want view.Row
	}{
		{
			name: "a workflow whose orchestrator is working says so on its own bar",
			row: agentRow{
				header:   true,
				workflow: workflowstore.Workflow{ID: "w1", Name: "weave"},
				orchestrator: &agentItem{
					agent: agentstore.Agent{ID: "o1", State: agentstore.StateThinking},
					alive: true,
				},
			},
			want: view.Row{
				Key:    "workflow:w1",
				Kind:   view.KindWorkflow,
				Name:   "weave",
				Status: view.StatusThinking,
			},
		},
		{
			name: "a workflow standing behind no orchestrator has no bar at all",
			row: agentRow{
				header:   true,
				workflow: workflowstore.Workflow{ID: "w1", Name: "weave"},
			},
			want: view.Row{
				Key:     "workflow:w1",
				Kind:    view.KindWorkflow,
				Name:    "weave",
				Dormant: true,
			},
		},
		{
			name: "a workflow whose orchestrator has stopped has no bar either",
			row: agentRow{
				header:       true,
				workflow:     workflowstore.Workflow{ID: "w1", Name: "weave"},
				orchestrator: &agentItem{agent: agentstore.Agent{ID: "o1", State: agentstore.StateIdle}},
			},
			want: view.Row{
				Key:     "workflow:w1",
				Kind:    view.KindWorkflow,
				Name:    "weave",
				Dormant: true,
			},
		},
		{
			name: "a child hangs off the row above it",
			row: agentRow{
				item:  agentItem{agent: agentstore.Agent{ID: "a1", Name: "reviewer"}},
				child: true,
			},
			want: view.Row{
				Key:  "agent:a1",
				Kind: view.KindChild,
				Name: "reviewer",
			},
		},
		{
			name: "an agent of its own is the ordinary row",
			row:  agentRow{item: agentItem{agent: agentstore.Agent{ID: "a1", Name: "worker"}}},
			want: view.Row{
				Key:  "agent:a1",
				Kind: view.KindAgent,
				Name: "worker",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, snapshotRowOf(t, snapshotParams{rows: []agentRow{tt.row}}))
		})
	}
}

// TestSnapshotHiddenTally pins the count a row stands in for, which is nothing at all once the
// rows it stands for are drawn themselves.
func TestSnapshotHiddenTally(t *testing.T) {
	tests := []struct {
		name       string
		showHidden bool
		count      int
		want       int
	}{
		{
			name:       "a count is dropped while the rows it stands for are drawn",
			showHidden: true,
			count:      3,
			want:       0,
		},
		{
			name:  "a count is kept while those rows are out of sight",
			count: 3,
			want:  3,
		},
		{
			name: "a row standing in for nothing counts nothing",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := snapshotRowOf(t, snapshotParams{
				rows:       []agentRow{{item: agentItem{agent: agentstore.Agent{ID: "a1"}}, hiddenCount: tt.count}},
				showHidden: tt.showHidden,
			})

			assert.Equal(t, tt.want, row.HiddenCount)
		})
	}
}

// TestSnapshotListing pins what the whole listing carries: the letters typed onto rows, the
// selection, the focused row and the column names are cut to.
func TestSnapshotListing(t *testing.T) {
	params := snapshotParams{
		rows: []agentRow{
			{header: true, workflow: workflowstore.Workflow{ID: "w1", Name: "weave"}},
			{item: agentItem{agent: agentstore.Agent{ID: "a1", Name: "picked"}}},
			{item: agentItem{agent: agentstore.Agent{ID: "a2", Name: "passed over"}}},
		},
		marks:     map[string]string{"workflow:w1": "w", "agent:a2": "p"},
		selected:  selection{ids: []string{"a1"}},
		focused:   "agent:a1",
		nameWidth: 12,
	}

	snapshot := agentsSnapshot(params)

	assert.Equal(t, "agent:a1", snapshot.Focused)
	assert.Equal(t, 12, snapshot.NameWidth)
	require.Len(t, snapshot.Rows, 3)
	assert.Equal(t, "w", snapshot.Rows[0].Mark)
	assert.Empty(t, snapshot.Rows[1].Mark)
	assert.Equal(t, "p", snapshot.Rows[2].Mark)
	assert.True(t, snapshot.Rows[1].Selected)
	assert.False(t, snapshot.Rows[2].Selected)
}
