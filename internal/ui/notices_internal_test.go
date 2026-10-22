package ui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/glyph"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/internal/view"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// TestAgentNotices covers what the record says about each mark a row carries, in the order a person would act on them.
func TestAgentNotices(t *testing.T) {
	tests := []struct {
		name string
		row  agentRow
		task panel.Task
		want []string
	}{
		{
			name: "an agent carrying nothing has nothing to say",
			row:  buildNoticeRow(panel.AgentRow{Alive: true}),
			want: []string{},
		},
		{
			name: "a reviewer leaves its task's removed directory to its writer's row",
			row: buildPlainReviewerRow(panel.AgentRow{Agent: panel.Agent{
				ID:   "a2",
				Role: panel.RoleReviewer,
			}}),
			task: panel.Task{WorktreeRemovalReason: panel.WorktreeGone},
			want: []string{},
		},
		{
			name: "a failed create",
			row:  buildNoticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate}}),
			want: []string{failedToCreateNotice},
		},
		{
			name: "a failed cleanup",
			row:  buildNoticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCleanup}}),
			want: []string{failedToCleanupNotice},
		},
		{
			name: "a turn an API error ended",
			row: buildNoticeRow(panel.AgentRow{
				Agent: panel.Agent{State: panel.StateErrored, TurnError: "Overloaded"},
				Alive: true,
			}),
			want: []string{turnFailedNotice + ": Overloaded"},
		},
		{
			name: "a turn an API error ended, on an agent somebody paused",
			row: buildNoticeRow(panel.AgentRow{
				Agent: panel.Agent{
					State:     panel.StateErrored,
					TurnError: "Overloaded",
					HeldAt:    xptr.Ptr(time.Now()),
				},
				Alive: true,
			}),
			want: []string{heldNotice},
		},
		{
			name: "a removed worktree",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: panel.Task{WorktreeRemovalReason: panel.WorktreeGone},
			want: []string{worktreeRemovedNotice},
		},
		{
			name: "a deleted worktree has nothing to say",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: panel.Task{WorktreeRemovalReason: panel.WorktreeDeleted},
			want: []string{},
		},
		{
			name: "a stopped rebase",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: panel.Task{Sequence: panel.SequenceRebase},
			want: []string{sequenceNotice},
		},
		{
			name: "a hidden task",
			row: agentRow{
				header: true,
				task:   &panel.Task{Hidden: true},
			},
			want: []string{hiddenNotice},
		},
		{
			name: "documents written",
			row:  buildNoticeRow(panel.AgentRow{Documents: 2}),
			want: []string{documentsNotice},
		},
		{
			name: "documents written for a task",
			row: agentRow{
				header: true,
				task:   &panel.Task{Documents: 2},
			},
			want: []string{documentsNotice},
		},
		{
			name: "documents written by an agent under its task header",
			row: agentRow{
				item:      panel.AgentRow{Documents: 2},
				underTask: true,
				workflow:  panel.Workflow{Worktree: panel.WorktreeConfig{Enabled: true}},
			},
			want: []string{},
		},
		{
			name: "unread replies on an agent under its task header",
			row:  agentRow{underTask: true},
			task: panel.Task{PersonComments: &agentv1.CommentCounts{Unread: 1}},
			want: []string{},
		},
		{
			name: "unread replies on a task",
			row: agentRow{
				header: true,
				task:   &panel.Task{PersonComments: &agentv1.CommentCounts{Unread: 1}},
			},
			want: []string{"your comments with a reply you have not read: 1"},
		},
		{
			want: []string{},
		},
		{
			name: "mail waiting",
			row:  buildNoticeRow(panel.AgentRow{UnreadMail: true}),
			want: []string{unreadMailNotice},
		},
		{
			name: "a failed script",
			row:  buildNoticeRow(panel.AgentRow{FailedRun: true}),
			want: []string{failedRunNotice},
		},
		{
			name: "a failing pull request",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: buildPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING),
			want: []string{"its pull request has a failing check"},
		},
		{
			name: "a conflicting pull request",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: buildPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING),
			want: []string{"its pull request no longer merges cleanly"},
		},
		{
			name: "a pull request with changes requested",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: buildPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED),
			want: []string{"its pull request has changes requested"},
		},
		{
			name: "a draft pull request",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: buildPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT),
			want: []string{"its pull request is still a draft"},
		},
		{
			name: "a draft merge request",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: panel.Task{PullRequest: panel.PullRequestStanding{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
				Forge: "gitlab",
			}},
			want: []string{"its merge request is still a draft"},
		},
		{
			name: "a branch with no pull request",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: buildPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE),
			want: []string{},
		},
		{
			name: "a failed create says where its worktree went",
			row:  buildNoticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate}}),
			task: panel.Task{WorktreeRemovalReason: panel.WorktreeGone},
			want: []string{failedToCreateNotice, worktreeRemovedNotice},
		},
		{
			name: "the marks a person would act on come first",
			row: buildNoticeRow(panel.AgentRow{
				Agent:      panel.Agent{State: panel.StateFailedToCleanup},
				UnreadMail: true,
				FailedRun:  true,
				Documents:  1,
			}),
			task: panel.Task{WorktreeRemovalReason: panel.WorktreeGone},
			want: []string{failedToCleanupNotice, failedRunNotice, unreadMailNotice, worktreeRemovedNotice, documentsNotice},
		},
		{
			name: "a pull request that needs a push comes before mail, and one that asks nothing comes last",
			row: buildNoticeRow(panel.AgentRow{
				UnreadMail: true,
				FailedRun:  true,
				Documents:  1,
			}),
			task: buildPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING),
			want: []string{failedRunNotice, "its pull request no longer merges cleanly", unreadMailNotice, documentsNotice},
		},
		{
			name: "an approved pull request comes after mail and before the directory",
			row: buildNoticeRow(panel.AgentRow{
				UnreadMail: true,
			}),
			task: panel.Task{
				WorktreeRemovalReason: panel.WorktreeGone,
				PullRequest:           panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED},
			},
			want: []string{unreadMailNotice, "its pull request is approved and ready to merge", worktreeRemovedNotice},
		},
		{
			name: "an open pull request comes after documents",
			row: buildNoticeRow(panel.AgentRow{
				Documents: 1,
			}),
			task: buildPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN),
			want: []string{documentsNotice, "its pull request is open and waiting on a review"},
		},
		{
			name: "a reviewer's unread replies are not counted",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: panel.Task{ReviewerComments: &agentv1.CommentCounts{Unread: 1}},
			want: []string{},
		},
		{
			name: "the person's comments with replies unread",
			row:  buildNoticeRow(panel.AgentRow{}),
			task: panel.Task{PersonComments: &agentv1.CommentCounts{Unread: 2}},
			want: []string{"your comments with a reply you have not read: 2"},
		},
		{
			name: "an archived row still counts its unread replies",
			row: buildNoticeRow(panel.AgentRow{
				Agent: panel.Agent{ArchivedAt: xptr.Ptr(time.Now())},
			}),
			task: panel.Task{PersonComments: &agentv1.CommentCounts{Unread: 1}},
			want: []string{"your comments with a reply you have not read: 1"},
		},
		{
			name: "an agent without a worktree still says its directory is gone",
			row:  buildPlainRow(panel.AgentRow{Agent: panel.Agent{ID: "a1"}}),
			task: panel.Task{WorktreeRemovalReason: panel.WorktreeGone},
			want: []string{worktreeRemovedNotice},
		},
		{
			name: "an agent without a worktree still says a rebase stopped in it",
			row:  buildPlainRow(panel.AgentRow{Agent: panel.Agent{ID: "a1"}}),
			task: panel.Task{Sequence: panel.SequenceRebase},
			want: []string{sequenceNotice},
		},
		{
			name: "an agent without a worktree still counts its unread replies",
			row:  buildPlainRow(panel.AgentRow{Agent: panel.Agent{ID: "a1"}}),
			task: panel.Task{PersonComments: &agentv1.CommentCounts{Unread: 1}},
			want: []string{"your comments with a reply you have not read: 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := tt.row
			row.item.Agent.TaskID = "t1"
			tasks := map[string]panel.Task{"t1": tt.task}

			assert.Equal(t, tt.want, readNoticeTexts(agentNotices(glyph.Defaults(), "", tasks, row)))
		})
	}
}

// TestAgentNoticesWearTheGlyphTheRowRenders covers each notice taking its glyph and tone from the cell the row renders.
func TestAgentNoticesWearTheGlyphTheRowRenders(t *testing.T) {
	item := panel.AgentRow{
		Agent: panel.Agent{
			State:  panel.StateWaiting,
			TaskID: "t1",
			HeldAt: xptr.Ptr(time.Now()),
		},
		Alive:      true,
		UnreadMail: true,
		FailedRun:  true,
		Documents:  3,
	}
	tasks := map[string]panel.Task{"t1": {Sequence: panel.SequenceRebase}}

	line := view.Prepare(view.Snapshot{Rows: []view.Row{snapshotRow(buildNoticeRow(item), snapshotParams{tasks: tasks})}}).Lines[0]

	want := map[string]view.Cell{
		failedRunNotice:    line.Alert,
		unreadMailNotice:   line.Mail,
		documentsNotice:    line.Document,
		sequenceNotice:     line.Columns[0],
		heldPromptedNotice: line.Held,
	}

	notices := agentNotices(glyph.Defaults(), "", tasks, buildNoticeRow(item))
	require.Len(t, notices, len(want))

	for _, notice := range notices {
		cell, ok := want[notice.Text]
		require.True(t, ok, "an unexpected notice %q", notice.Text)
		assert.Equal(t, view.Spell(glyph.Defaults(), cell), notice.Icon, "the glyph beside %q", notice.Text)
		assert.Equal(t, cell.Tone, notice.Tone, "the tone beside %q", notice.Text)
	}
}

// TestAgentNoticesSpeakForEveryColumnState covers every state a column renders having a sentence, or being named as its baseline or drawn empty.
func TestAgentNoticesSpeakForEveryColumnState(t *testing.T) {
	for state := range int(view.RepoEnd) {
		repo := view.Repo(state)
		_, said := repoSaid[repo]

		assert.NotEqual(t, said, repo == view.RepoClean || repo == view.RepoDeleted, "repo state %d is neither given a sentence, its baseline nor drawn empty", state)
	}

	for state := range int(view.ForgeEnd) {
		forge := view.Forge(state)
		said := 0
		for _, sentences := range []map[view.Forge]string{forgeAlertSaid, forgeReviewSaid, forgeSaid} {
			if _, ok := sentences[forge]; ok {
				said++
			}
		}

		want := 0
		if !view.ForgeCell(view.Row{
			Kind:        view.KindAgent,
			Forge:       forge,
			LoaderFrame: "⠋",
		}).IsBlank() {
			want = 1
		}

		assert.Equal(t, want, said, "forge state %d is given %d sentences", state, said)
	}

	for _, state := range []panel.State{panel.StateFailedToCreate, panel.StateFailedToCleanup} {
		require.Equal(t, view.StatusBroken, snapshotStatus(panel.AgentRow{Agent: panel.Agent{State: state}}), "state %q no longer renders broken", state)
		assert.NotEmpty(t, failureSaid[state], "state %q renders broken with no sentence", state)
	}
}

// TestAgentNoticesLeaveAStoppedRowToItsName covers a row whose process has gone saying so with its grey name alone.
func TestAgentNoticesLeaveAStoppedRowToItsName(t *testing.T) {
	stopped := buildNoticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateIdle}})
	row := snapshotRow(stopped, snapshotParams{})

	require.Equal(t, view.ProcessStopped, row.Process)
	assert.Equal(t, view.EmphasisDim, view.Prepare(view.Snapshot{Rows: []view.Row{row}}).Lines[0].Emphasis)
	assert.Empty(t, readNoticeTexts(agentNotices(glyph.Defaults(), "", nil, stopped)))

	failed := buildNoticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate}})

	require.Equal(t, view.ProcessAlive, snapshotRow(failed, snapshotParams{}).Process, "a failed create is not a stopped process")
	assert.Contains(t, readNoticeTexts(agentNotices(glyph.Defaults(), "", nil, failed)), failedToCreateNotice)
}

func buildNoticeRow(item panel.AgentRow) agentRow {
	return agentRow{
		item:     item,
		workflow: panel.Workflow{Worktree: panel.WorktreeConfig{Enabled: true}},
	}
}

func buildPullRequested(state pullrequestv1.PullRequestState) panel.Task {
	return panel.Task{PullRequest: panel.PullRequestStanding{State: state}}
}

func buildPlainRow(item panel.AgentRow) agentRow {
	item.Alive = true

	return agentRow{
		item: item,
		workflow: panel.Workflow{
			ID:   "w1",
			Name: "komrad",
		},
	}
}

func buildPlainReviewerRow(item panel.AgentRow) agentRow {
	row := buildPlainRow(item)
	row.underTask = true

	return row
}

func readNoticeTexts(notices []record.Notice) []string {
	return xslice.Map(notices, func(notice record.Notice) string {
		return notice.Text
	})
}
