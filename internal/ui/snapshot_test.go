package ui

import (
	"regexp"
	"testing"
	"time"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/view"
	"weave/lib/xptr"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkoutWorkflow is a workflow weave cuts a worktree from, which is what gives the agents
// under it a checkout of their own.
func checkoutWorkflow() workflowstore.Workflow {
	return workflowstore.Workflow{
		ID:       "w1",
		Name:     "weave",
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	}
}

// snapshotOne reads one agent row and hands back the single row it made.
func snapshotOne(t *testing.T, item agentItem) view.Row {
	t.Helper()

	return snapshotRowOf(t, snapshotParams{rows: []agentRow{{item: item, workflow: checkoutWorkflow()}}})
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

// TestSnapshotReview pins the five states the column picks from the last conclusion released
// and whether the worktree still matches the round it was given on.
func TestSnapshotReview(t *testing.T) {
	tests := []struct {
		name string
		item agentItem
		want view.Review
	}{
		{
			name: "an approval on the tree as it stands is approved",
			item: agentItem{localVerdict: string(reviewstore.VerdictApproved)},
			want: view.ReviewApproved,
		},
		{
			name: "an approval the tree has moved past is owed another pass",
			item: agentItem{localVerdict: string(reviewstore.VerdictApproved), localTree: git.StandingMoved},
			want: view.ReviewApprovedMoved,
		},
		{
			name: "changes requested on the tree as it stands are still owed by the agent",
			item: agentItem{localVerdict: string(reviewstore.VerdictChangesRequested)},
			want: view.ReviewChangesRequested,
		},
		{
			name: "changes requested the tree has moved past are ready to read again",
			item: agentItem{localVerdict: string(reviewstore.VerdictChangesRequested), localTree: git.StandingMoved},
			want: view.ReviewChangesPushed,
		},
		{
			name: "an approval the base has taken, with nothing on top, is approved and landed",
			item: agentItem{localVerdict: string(reviewstore.VerdictApproved), localTree: git.StandingLanded},
			want: view.ReviewApprovedLanded,
		},
		{
			name: "an approval the base has taken, with new work on top, is never reviewed for that work",
			item: agentItem{localVerdict: string(reviewstore.VerdictApproved), localTree: git.StandingLandedMoved},
			want: view.ReviewApprovedLandedMoved,
		},
		{
			name: "changes requested on a tree the base took as it was are finished business",
			item: agentItem{localVerdict: string(reviewstore.VerdictChangesRequested), localTree: git.StandingLanded},
			want: view.ReviewChangesLanded,
		},
		{
			name: "changes requested on a landed tree with new work on top are never reviewed for that work",
			item: agentItem{localVerdict: string(reviewstore.VerdictChangesRequested), localTree: git.StandingLandedMoved},
			want: view.ReviewChangesLandedMoved,
		},
		{
			name: "notes without a conclusion are no review",
			item: agentItem{localVerdict: string(reviewstore.VerdictCommented)},
			want: view.ReviewNone,
		},
		{
			name: "work nobody has concluded on has no review",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := agentItem{
				agent: agentstore.Agent{
					ID:       "a1",
					Metadata: []agentstore.Metadata{{Key: "pr", Value: "https://example.test/1"}},
				},
				pullRequest: pullRequestStanding{state: tt.state},
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
			name: "a hidden agent carries the fact as a mark of its own",
			item: agentItem{agent: agentstore.Agent{
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
			item: agentItem{agent: agentstore.Agent{
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
			name: "output nobody has read carries through as unviewed",
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

// TestSnapshotProcess pins the difference between an agent sitting quiet and one whose process
// has stopped, which is the one thing the state it wears cannot say.
func TestSnapshotProcess(t *testing.T) {
	tests := []struct {
		name string
		item agentItem
		want view.Process
	}{
		{
			name: "an agent whose pane has gone has nothing behind it",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateIdle}},
			want: view.ProcessStopped,
		},
		{
			name: "an agent that stopped mid-thought has nothing behind it either",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateThinking}},
			want: view.ProcessStopped,
		},
		{
			name: "a create that failed is not dormant, since the row still has a failure to report",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateFailedToCreate}},
			want: view.ProcessAlive,
		},
		{
			name: "an agent still being set up is not dormant, since it is on its way",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateSettingUp}},
			want: view.ProcessAlive,
		},
		{
			name: "an agent sitting quiet with a pane of its own is not dormant",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateIdle}, alive: true},
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
				Process: view.ProcessNone,
			},
		},
		{
			name: "a workflow whose orchestrator has stopped has nothing running behind it",
			row: agentRow{
				header:       true,
				workflow:     workflowstore.Workflow{ID: "w1", Name: "weave"},
				orchestrator: &agentItem{agent: agentstore.Agent{ID: "o1", State: agentstore.StateIdle}},
			},
			want: view.Row{
				Key:     "workflow:w1",
				Kind:    view.KindWorkflow,
				Name:    "weave",
				Process: view.ProcessNone,
			},
		},
		{
			name: "a workflow whose orchestrator failed to start still wears the failure",
			row: agentRow{
				header:       true,
				workflow:     workflowstore.Workflow{ID: "w1", Name: "weave"},
				orchestrator: &agentItem{agent: agentstore.Agent{ID: "o1", State: agentstore.StateFailedToCreate}},
			},
			want: view.Row{
				Key:    "workflow:w1",
				Kind:   view.KindWorkflow,
				Name:   "weave",
				Status: view.StatusBroken,
			},
		},
		{
			name: "a workflow a schedule launches into says so",
			row: agentRow{
				header:   true,
				workflow: workflowstore.Workflow{ID: "w1", Name: "weave"},
			},
			scheduled: map[string]bool{"w1": true},
			want: view.Row{
				Key:       "workflow:w1",
				Kind:      view.KindWorkflow,
				Name:      "weave",
				Process:   view.ProcessNone,
				Scheduled: true,
			},
		},
		{
			name: "an agent under a scheduled workflow does not inherit its mark",
			row: agentRow{
				workflow: checkoutWorkflow(),
				item: agentItem{
					agent: agentstore.Agent{
						ID:         "a1",
						Name:       "worker",
						WorkflowID: "w1",
						State:      agentstore.StateIdle,
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
				item: agentItem{agent: agentstore.Agent{
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
			row:  agentRow{workflow: checkoutWorkflow(), item: agentItem{agent: agentstore.Agent{ID: "a1", Name: "worker"}}},
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
// still reports the directory it was handed and the review of its work.
func TestSnapshotPlainWorkflowKeepsItsColumns(t *testing.T) {
	row := snapshotRowOf(t, snapshotParams{rows: []agentRow{{
		workflow: workflowstore.Workflow{ID: "w1", Name: "weave"},
		item: agentItem{
			alive:        true,
			localVerdict: string(reviewstore.VerdictApproved),
			agent: agentstore.Agent{
				ID:              "a1",
				Name:            "worker",
				WorkflowID:      "w1",
				Cwd:             "/tmp/somewhere",
				State:           agentstore.StateIdle,
				WorktreeRemoved: true,
			},
		},
	}}})

	assert.Equal(t, view.RepoGone, row.Repo)
	assert.Equal(t, view.ReviewApproved, row.Review)
	assert.False(t, view.RepoCell(row).IsBlank())
	assert.False(t, view.ReviewCell(row).IsBlank())
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
		marks:    map[string]string{"workflow:w1": "w", "agent:a2": "p"},
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
			row:  agentRow{item: agentItem{agent: agentstore.Agent{ID: "a1", Name: "view-gather"}}},
			want: nil,
		},
		{
			name: "a search matching twice marks both",
			expr: "view",
			row:  agentRow{item: agentItem{agent: agentstore.Agent{ID: "a1", Name: "view-of-a-view"}}},
			want: []view.Span{{Start: 0, End: 4}, {Start: 10, End: 14}},
		},
		{
			name: "a workflow name is searched the same way",
			expr: "eave",
			row:  agentRow{header: true, workflow: workflowstore.Workflow{ID: "w1", Name: "weave"}},
			want: []view.Span{{Start: 1, End: 5}},
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
	row := snapshotOne(t, agentItem{agent: agentstore.Agent{ID: "a1", Name: "view-gather"}})

	assert.Nil(t, row.Matches)
}

// TestSnapshotHeldWorking pins the pause that did not take: weave goes on reading the pane, so a
// paused agent that is working again says so rather than the row claiming it stopped.
func TestSnapshotHeldWorking(t *testing.T) {
	tests := []struct {
		name string
		item agentItem
		want bool
	}{
		{
			name: "a paused agent that started a turn is working anyway",
			item: agentItem{
				agent: agentstore.Agent{State: agentstore.StateThinking, HeldAt: xptr.Ptr(time.Now())},
				alive: true,
			},
			want: true,
		},
		{
			name: "so is one stopped at a prompt, where an answer would land a write",
			item: agentItem{
				agent: agentstore.Agent{State: agentstore.StateWaiting, HeldAt: xptr.Ptr(time.Now())},
				alive: true,
			},
			want: true,
		},
		{
			name: "a pane nobody can read says nothing either way, so the hold stands",
			item: agentItem{
				agent: agentstore.Agent{State: agentstore.StateUncertain, HeldAt: xptr.Ptr(time.Now())},
				alive: true,
			},
			want: false,
		},
		{
			name: "a paused agent sitting quiet is held and nothing more",
			item: agentItem{
				agent: agentstore.Agent{State: agentstore.StateIdle, HeldAt: xptr.Ptr(time.Now())},
				alive: true,
			},
			want: false,
		},
		{
			name: "an agent nobody paused is never working through a hold",
			item: agentItem{agent: agentstore.Agent{State: agentstore.StateThinking}, alive: true},
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
	rows := []agentRow{{item: agentItem{agent: agentstore.Agent{ID: "a1", Name: "worker"}}}}

	hidden := agentsSnapshot(snapshotParams{
		rows:        rows,
		focused:     "agent:a1",
		caretHidden: true,
	})
	shown := agentsSnapshot(snapshotParams{rows: rows, focused: "agent:a1"})

	assert.True(t, hidden.CaretHidden)
	assert.False(t, shown.CaretHidden)
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
	agent := agentstore.Agent{
		ID:             "a1",
		Name:           "worker",
		ReportedReason: "reading internal/view",
	}

	fields := agentFields(agentFieldsParams{agent: agent})

	assert.Contains(t, fields, recordField{"doing", "reading internal/view"})
	assert.NotContains(t, agentFields(agentFieldsParams{agent: agentstore.Agent{ID: "a1"}}), recordField{"doing", ""})
}

// TestSnapshotChildRowCarriesNoTally pins half of why the busiest row a panel can draw is
// narrower than the busiest row the model can express: a hidden child is counted on the parent
// drawn above it, so a child row never stands in for anything.
func TestSnapshotChildRowCarriesNoTally(t *testing.T) {
	parent := agentstore.Agent{
		ID:         "a1",
		Name:       "parent",
		WorkflowID: "w1",
		State:      agentstore.StateIdle,
	}
	child := agentstore.Agent{
		ID:            "a2",
		Name:          "child",
		WorkflowID:    "w1",
		State:         agentstore.StateIdle,
		Hidden:        true,
		ParentAgentID: xptr.Ptr("a1"),
	}

	rows := treeRows(treeRowsParams{
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "weave"}},
		agents:    []agentItem{{agent: parent}, {agent: child}},
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
