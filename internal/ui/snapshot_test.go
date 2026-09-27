package ui

import (
	"regexp"
	"testing"
	"time"

	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkoutWorkflow is a workflow komrad cuts a worktree from.
func checkoutWorkflow() panel.Workflow {
	return panel.Workflow{
		ID:       "w1",
		Name:     "komrad",
		Worktree: panel.WorktreeConfig{Enabled: true},
	}
}

// snapshotOne reads one agent row and hands back the single row it made.
func snapshotOne(t *testing.T, item panel.AgentRow) view.Row {
	t.Helper()

	return snapshotRowOf(t, snapshotParams{rows: []agentRow{{item: item, workflow: checkoutWorkflow()}}})
}

// snapshotRunning reads one row whose agent is running and doing nothing.
func snapshotRunning(t *testing.T, item panel.AgentRow) view.Row {
	t.Helper()

	item.Alive = true
	item.Agent.State = panel.StateIdle

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
		item panel.AgentRow
		want view.Status
	}{
		{
			name: "a create that failed is broken",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate}},
			want: view.StatusBroken,
		},
		{
			name: "a teardown that failed is broken",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCleanup}},
			want: view.StatusBroken,
		},
		{
			name: "an agent stopped at a prompt wants you",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateWaiting}, Alive: true},
			want: view.StatusWaiting,
		},
		{
			name: "a pane nobody can read wants you, since nobody else will look",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateUncertain}, Alive: true},
			want: view.StatusWaiting,
		},
		{
			name: "an agent somebody paused wants that person back, whatever it was doing",
			item: panel.AgentRow{
				Agent: panel.Agent{State: panel.StateThinking, HeldAt: xptr.Ptr(time.Now())},
				Alive: true,
			},
			want: view.StatusWaiting,
		},
		{
			name: "an agent whose pane has gone is idle whatever it stopped in",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateThinking}},
			want: view.StatusIdle,
		},
		{
			name: "an agent still being set up is under way, with nobody blocked",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateSettingUp}},
			want: view.StatusThinking,
		},
		{
			name: "a running agent is thinking",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateThinking}, Alive: true},
			want: view.StatusThinking,
		},
		{
			name: "an agent doing nothing is idle",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateIdle}, Alive: true},
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
		agent panel.Agent
		want  view.Repo
	}{
		{
			name:  "a directory that has gone beats a rebase left in it",
			agent: panel.Agent{WorktreeRemoved: true, SequenceInProgress: true},
			want:  view.RepoGone,
		},
		{
			name:  "a create that failed with a directory that has gone still says so",
			agent: panel.Agent{State: panel.StateFailedToCreate, WorktreeRemoved: true},
			want:  view.RepoGone,
		},
		{
			name:  "a create that failed before there was a directory reads as clean",
			agent: panel.Agent{State: panel.StateFailedToCreate},
			want:  view.RepoClean,
		},
		{
			name:  "a rebase left part way through says so",
			agent: panel.Agent{SequenceInProgress: true},
			want:  view.RepoRebasing,
		},
		{
			name:  "an untouched checkout is clean",
			agent: panel.Agent{},
			want:  view.RepoClean,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, snapshotOne(t, panel.AgentRow{Agent: tt.agent}).Repo)
		})
	}
}

// TestSnapshotComments pins each side's counts reaching the row as the listing carried them,
// an archived agent's included.
func TestSnapshotComments(t *testing.T) {
	tests := []struct {
		name         string
		item         panel.AgentRow
		wantReviewer *view.Comments
		wantPerson   *view.Comments
	}{
		{
			name: "work nobody has commented on counts nothing",
			item: panel.AgentRow{},
		},
		{
			name: "an archived agent keeps its counts",
			item: panel.AgentRow{
				Agent:          panel.Agent{ArchivedAt: xptr.Ptr(time.Now())},
				PersonComments: &agentv1.CommentCounts{Resolved: 2},
			},
			wantPerson: &view.Comments{Resolved: 2},
		},
		{
			name: "each side is counted apart",
			item: panel.AgentRow{
				ReviewerComments: &agentv1.CommentCounts{
					Open:   1,
					Unread: true,
				},
				PersonComments: &agentv1.CommentCounts{
					Open:     2,
					Resolved: 1,
				},
			},
			wantReviewer: &view.Comments{
				Open:   1,
				Unread: true,
			},
			wantPerson: &view.Comments{
				Open:     2,
				Resolved: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := snapshotOne(t, tt.item)

			assert.Equal(t, tt.wantReviewer, row.ReviewerComments)
			assert.Equal(t, tt.wantPerson, row.PersonComments)
		})
	}
}

// TestSnapshotForge pins the pull request column reading the standing the listing carries and
// nothing else, every wire state mapped to its own mark and a recorded pr key ignored.
func TestSnapshotForge(t *testing.T) {
	tests := []struct {
		name  string
		state pullrequestv1.PullRequestState
		want  view.Forge
	}{
		{
			name:  "nothing decided is unread",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED,
			want:  view.ForgeUnread,
		},
		{
			name:  "unread",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD,
			want:  view.ForgeUnread,
		},
		{
			name:  "none",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE,
			want:  view.ForgeNone,
		},
		{
			name:  "open",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
			want:  view.ForgeOpen,
		},
		{
			name:  "approved",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
			want:  view.ForgeApproved,
		},
		{
			name:  "changes requested",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
			want:  view.ForgeChangesRequested,
		},
		{
			name:  "draft",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
			want:  view.ForgeDraft,
		},
		{
			name:  "merged",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
			want:  view.ForgeMerged,
		},
		{
			name:  "closed",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
			want:  view.ForgeClosed,
		},
		{
			name:  "failing",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
			want:  view.ForgeFailing,
		},
		{
			name:  "conflicting",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING,
			want:  view.ForgeConflicting,
		},
		{
			name:  "approved waiting",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
			want:  view.ForgeApprovedWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := panel.AgentRow{
				Agent: panel.Agent{
					ID: "a1",
				},
				PullRequest: panel.PullRequestStanding{State: tt.state},
			}

			assert.Equal(t, tt.want, snapshotOne(t, item).Forge)
		})
	}

	assert.Len(t, tests, len(pullrequestv1.PullRequestState_name), "every wire state is pinned")
}

// TestSnapshotMarksOnARow pins the marks a row carries beside its name.
func TestSnapshotMarksOnARow(t *testing.T) {
	tests := []struct {
		name string
		item panel.AgentRow
		want view.Row
	}{
		{
			name: "an agent somebody is holding says it is paused",
			item: panel.AgentRow{Agent: panel.Agent{
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
			item: panel.AgentRow{
				Agent:     panel.Agent{ID: "a1", Name: "alert"},
				FailedRun: true,
			},
			want: view.Row{
				Key:       "agent:a1",
				Name:      "alert",
				FailedRun: true,
			},
		},
		{
			name: "mail it has not read says so",
			item: panel.AgentRow{
				Agent:      panel.Agent{ID: "a1", Name: "mail"},
				UnreadMail: true,
			},
			want: view.Row{
				Key:        "agent:a1",
				Name:       "mail",
				UnreadMail: true,
			},
		},
		{
			name: "an agent a schedule launched says so",
			item: panel.AgentRow{Agent: panel.Agent{
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
			name: "a hidden agent carries the fact as a mark of its own",
			item: panel.AgentRow{Agent: panel.Agent{
				ID:     "a1",
				Name:   "hidden",
				Hidden: true,
			}},
			want: view.Row{
				Key:    "agent:a1",
				Name:   "hidden",
				Hidden: true,
			},
		},
		{
			name: "an archived agent says so rather than being greyed by hand",
			item: panel.AgentRow{Agent: panel.Agent{
				ID:         "a1",
				Name:       "put-away",
				ArchivedAt: xptr.Ptr(time.Now()),
			}},
			want: view.Row{
				Key:      "agent:a1",
				Name:     "put-away",
				Archived: true,
			},
		},
		{
			name: "an ordinary agent carries nothing at all",
			item: panel.AgentRow{Agent: panel.Agent{ID: "a1", Name: "ordinary"}},
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

// TestSnapshotProcess pins the difference between an agent sitting quiet and one whose process has
// stopped.
func TestSnapshotProcess(t *testing.T) {
	tests := []struct {
		name string
		item panel.AgentRow
		want view.Process
	}{
		{
			name: "an agent whose pane has gone has nothing behind it",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateIdle}},
			want: view.ProcessStopped,
		},
		{
			name: "an agent that stopped mid-thought has nothing behind it either",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateThinking}},
			want: view.ProcessStopped,
		},
		{
			name: "a create that failed is not dormant, since the row still has a failure to report",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate}},
			want: view.ProcessAlive,
		},
		{
			name: "an agent still being set up is not dormant, since it is on its way",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateSettingUp}},
			want: view.ProcessAlive,
		},
		{
			name: "an agent sitting quiet with a pane of its own is not dormant",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateIdle}, Alive: true},
			want: view.ProcessAlive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, snapshotOne(t, tt.item).Process)
		})
	}
}

// TestSnapshotKinds pins what each row of the tree stands for.
func TestSnapshotKinds(t *testing.T) {
	tests := []struct {
		name      string
		row       agentRow
		scheduled map[string]bool
		want      view.Row
	}{
		{
			name: "a workflow whose orchestrator is working says so on its own bar",
			row: agentRow{
				header:   true,
				workflow: panel.Workflow{ID: "w1", Name: "komrad"},
				orchestrator: &panel.AgentRow{
					Agent: panel.Agent{ID: "o1", State: panel.StateThinking},
					Alive: true,
				},
			},
			want: view.Row{
				Key:    "workflow:w1",
				Kind:   view.KindWorkflow,
				Name:   "komrad",
				Status: view.StatusThinking,
			},
		},
		{
			name: "a workflow standing behind no orchestrator has no bar at all",
			row: agentRow{
				header:   true,
				workflow: panel.Workflow{ID: "w1", Name: "komrad"},
			},
			want: view.Row{
				Key:     "workflow:w1",
				Kind:    view.KindWorkflow,
				Name:    "komrad",
				Process: view.ProcessNone,
			},
		},
		{
			name: "a workflow whose orchestrator has stopped has nothing running behind it",
			row: agentRow{
				header:       true,
				workflow:     panel.Workflow{ID: "w1", Name: "komrad"},
				orchestrator: &panel.AgentRow{Agent: panel.Agent{ID: "o1", State: panel.StateIdle}},
			},
			want: view.Row{
				Key:     "workflow:w1",
				Kind:    view.KindWorkflow,
				Name:    "komrad",
				Process: view.ProcessNone,
			},
		},
		{
			name: "a workflow wears the mail waiting for its orchestrator",
			row: agentRow{
				header:   true,
				workflow: panel.Workflow{ID: "w1", Name: "komrad"},
				orchestrator: &panel.AgentRow{
					Agent:      panel.Agent{ID: "o1", State: panel.StateIdle},
					UnreadMail: true,
				},
			},
			want: view.Row{
				Key:        "workflow:w1",
				Kind:       view.KindWorkflow,
				Name:       "komrad",
				Process:    view.ProcessNone,
				UnreadMail: true,
			},
		},
		{
			name: "a workflow whose orchestrator failed to start still wears the failure",
			row: agentRow{
				header:       true,
				workflow:     panel.Workflow{ID: "w1", Name: "komrad"},
				orchestrator: &panel.AgentRow{Agent: panel.Agent{ID: "o1", State: panel.StateFailedToCreate}},
			},
			want: view.Row{
				Key:    "workflow:w1",
				Kind:   view.KindWorkflow,
				Name:   "komrad",
				Status: view.StatusBroken,
			},
		},
		{
			name: "a workflow a schedule launches into says so",
			row: agentRow{
				header:   true,
				workflow: panel.Workflow{ID: "w1", Name: "komrad"},
			},
			scheduled: map[string]bool{"w1": true},
			want: view.Row{
				Key:       "workflow:w1",
				Kind:      view.KindWorkflow,
				Name:      "komrad",
				Process:   view.ProcessNone,
				Scheduled: true,
			},
		},
		{
			name: "an agent under a scheduled workflow does not inherit its mark",
			row: agentRow{
				workflow: checkoutWorkflow(),
				item: panel.AgentRow{
					Agent: panel.Agent{
						ID:         "a1",
						Name:       "worker",
						WorkflowID: "w1",
						State:      panel.StateIdle,
					},
				},
			},
			scheduled: map[string]bool{"w1": true},
			want: view.Row{
				Key:     "agent:a1",
				Kind:    view.KindAgent,
				Name:    "worker",
				Process: view.ProcessStopped,
			},
		},
		{
			name: "a child hangs off the row above it",
			row: agentRow{
				workflow: checkoutWorkflow(),
				item: panel.AgentRow{Agent: panel.Agent{
					ID:            "a1",
					Name:          "reviewer",
					ParentAgentID: xptr.Ptr("a0"),
				}},
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
			row:  agentRow{workflow: checkoutWorkflow(), item: panel.AgentRow{Agent: panel.Agent{ID: "a1", Name: "worker"}}},
			want: view.Row{
				Key:  "agent:a1",
				Kind: view.KindAgent,
				Name: "worker",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, snapshotRowOf(t, snapshotParams{
				rows:      []agentRow{tt.row},
				scheduled: tt.scheduled,
			}))
		})
	}
}

// TestSnapshotPlainWorkflowKeepsItsColumns pins that an agent in a workflow cutting no worktrees
// still reports the directory it was handed and the comments on its work.
func TestSnapshotPlainWorkflowKeepsItsColumns(t *testing.T) {
	row := snapshotRowOf(t, snapshotParams{rows: []agentRow{{
		workflow: panel.Workflow{ID: "w1", Name: "komrad"},
		item: panel.AgentRow{
			Alive:          true,
			PersonComments: &agentv1.CommentCounts{Open: 1},
			Agent: panel.Agent{
				ID:              "a1",
				Name:            "worker",
				WorkflowID:      "w1",
				Cwd:             "/tmp/somewhere",
				State:           panel.StateIdle,
				WorktreeRemoved: true,
			},
		},
	}}})

	assert.Equal(t, view.RepoGone, row.Repo)
	assert.Equal(t, &view.Comments{Open: 1}, row.PersonComments)
	assert.False(t, view.RepoCell(row).IsBlank())
}

// TestSnapshotHiddenTally pins the count a row stands in for.
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
				rows:       []agentRow{{item: panel.AgentRow{Agent: panel.Agent{ID: "a1"}}, hiddenCount: tt.count}},
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
			{header: true, workflow: panel.Workflow{ID: "w1", Name: "komrad"}},
			{item: panel.AgentRow{Agent: panel.Agent{ID: "a1", Name: "picked"}}},
			{item: panel.AgentRow{Agent: panel.Agent{ID: "a2", Name: "passed over"}}},
		},
		marks:    map[string]string{"w1": "w", "a2": "p"},
		selected: selectAgents([]string{"a1"}...),
		focused:  "agent:a1",
		pane:     40,
	}

	snapshot := agentsSnapshot(params)

	assert.Equal(t, "agent:a1", snapshot.Focused)
	assert.Equal(t, 40, snapshot.Pane)
	require.Len(t, snapshot.Rows, 3)
	assert.Equal(t, "w", snapshot.Rows[0].Mark)
	assert.Empty(t, snapshot.Rows[1].Mark)
	assert.Equal(t, "p", snapshot.Rows[2].Mark)
	assert.True(t, snapshot.Rows[1].Selected)
	assert.False(t, snapshot.Rows[2].Selected)
}

// TestSnapshotSearchMatches pins what a filtered list carries about what it matched on, which is
// where the matches are rather than any colour for them.
func TestSnapshotSearchMatches(t *testing.T) {
	tests := []struct {
		name string
		expr string
		row  agentRow
		want []view.Span
	}{
		{
			name: "a search matching nothing in the name leaves it unmarked",
			expr: "nothing",
			row:  agentRow{item: panel.AgentRow{Agent: panel.Agent{ID: "a1", Name: "view-gather"}}},
			want: nil,
		},
		{
			name: "a search matching twice marks both",
			expr: "view",
			row:  agentRow{item: panel.AgentRow{Agent: panel.Agent{ID: "a1", Name: "view-of-a-view"}}},
			want: []view.Span{{Start: 0, End: 4}, {Start: 10, End: 14}},
		},
		{
			name: "a workflow name is searched the same way",
			expr: "omrad",
			row:  agentRow{header: true, workflow: panel.Workflow{ID: "w1", Name: "komrad"}},
			want: []view.Span{{Start: 1, End: 6}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := snapshotRowOf(t, snapshotParams{
				rows:    []agentRow{tt.row},
				matcher: regexp.MustCompile(tt.expr),
			})

			assert.Equal(t, tt.want, row.Matches)
		})
	}
}

// TestSnapshotSearchOffMarksNothing pins a list nobody is searching carrying no matches at all.
func TestSnapshotSearchOffMarksNothing(t *testing.T) {
	row := snapshotOne(t, panel.AgentRow{Agent: panel.Agent{ID: "a1", Name: "view-gather"}})

	assert.Nil(t, row.Matches)
}

// TestSnapshotHeldWorking pins the pause that did not take: komrad goes on reading the pane, so a
// paused agent that is working again says so rather than the row claiming it stopped.
func TestSnapshotHeldWorking(t *testing.T) {
	tests := []struct {
		name string
		item panel.AgentRow
		want bool
	}{
		{
			name: "a paused agent that started a turn is working anyway",
			item: panel.AgentRow{
				Agent: panel.Agent{State: panel.StateThinking, HeldAt: xptr.Ptr(time.Now())},
				Alive: true,
			},
			want: true,
		},
		{
			name: "so is one stopped at a prompt, where an answer would land a write",
			item: panel.AgentRow{
				Agent: panel.Agent{State: panel.StateWaiting, HeldAt: xptr.Ptr(time.Now())},
				Alive: true,
			},
			want: true,
		},
		{
			name: "a pane nobody can read says nothing either way, so the hold stands",
			item: panel.AgentRow{
				Agent: panel.Agent{State: panel.StateUncertain, HeldAt: xptr.Ptr(time.Now())},
				Alive: true,
			},
			want: false,
		},
		{
			name: "a paused agent sitting quiet is held and nothing more",
			item: panel.AgentRow{
				Agent: panel.Agent{State: panel.StateIdle, HeldAt: xptr.Ptr(time.Now())},
				Alive: true,
			},
			want: false,
		},
		{
			name: "an agent nobody paused is never working through a hold",
			item: panel.AgentRow{Agent: panel.Agent{State: panel.StateThinking}, Alive: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, snapshotOne(t, tt.item).HeldWorking)
		})
	}
}

// TestSnapshotCarriesTheBlink pins the caret's blink reaching the view as a fact rather than the
// view inventing one, the focused row being named either way.
func TestSnapshotCarriesTheBlink(t *testing.T) {
	rows := []agentRow{{item: panel.AgentRow{Agent: panel.Agent{ID: "a1", Name: "worker"}}}}

	hidden := agentsSnapshot(snapshotParams{
		rows:    rows,
		focused: "agent:a1",
		caret:   view.CaretBlinkedOut,
	})
	shown := agentsSnapshot(snapshotParams{rows: rows, focused: "agent:a1"})

	assert.Equal(t, view.CaretBlinkedOut, hidden.Caret)
	assert.Equal(t, view.CaretDrawn, shown.Caret)
	assert.Equal(t, "agent:a1", hidden.Focused)
}

// TestSnapshotOfAnEmptyPanelIsEmpty pins the first run, where there is nothing to draw at all.
func TestSnapshotOfAnEmptyPanelIsEmpty(t *testing.T) {
	snapshot := agentsSnapshot(snapshotParams{})

	assert.Empty(t, snapshot.Rows)
}

// TestReportedReasonSurvivesInTheRecord pins the one thing the new row drops that nothing else
// was showing: what the agent's own CLI last said it was doing.
func TestReportedReasonSurvivesInTheRecord(t *testing.T) {
	agent := panel.Agent{
		ID:             "a1",
		Name:           "worker",
		ReportedReason: "reading internal/view",
	}

	fields := agentFields(agentFieldsParams{agent: agent})

	assert.Contains(t, fields, recordField{"doing", "reading internal/view"})
	assert.NotContains(t, agentFields(agentFieldsParams{agent: panel.Agent{ID: "a1"}}), recordField{"doing", ""})
}

// TestSnapshotChildRowCarriesNoTally pins half of why the busiest row a panel can draw is
// narrower than the busiest row the model can express: a hidden child is counted on the parent
// drawn above it, so a child row never stands in for anything.
func TestSnapshotChildRowCarriesNoTally(t *testing.T) {
	parent := panel.Agent{
		ID:         "a1",
		Name:       "parent",
		WorkflowID: "w1",
		State:      panel.StateIdle,
	}
	child := panel.Agent{
		ID:            "a2",
		Name:          "child",
		WorkflowID:    "w1",
		State:         panel.StateIdle,
		Hidden:        true,
		ParentAgentID: xptr.Ptr("a1"),
	}

	rows := treeRows(treeRowsParams{
		workflows: []panel.Workflow{{ID: "w1", Name: "komrad"}},
		agents:    []panel.AgentRow{{Agent: parent}, {Agent: child}},
	})

	snapshot := agentsSnapshot(snapshotParams{rows: rows})
	require.NotEmpty(t, snapshot.Rows)

	for _, row := range snapshot.Rows {
		if row.Kind == view.KindChild {
			assert.Zero(t, row.HiddenCount, "a child row stands in for nothing")
		}
	}

	assert.Equal(t, 1, snapshot.Rows[1].HiddenCount, "the count lands on the parent instead")
}
