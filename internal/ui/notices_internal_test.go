package ui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// TestAgentNotices covers what the record says about each mark a row carries, in the order a person would act on them.
func TestAgentNotices(t *testing.T) {
	tests := []struct {
		name string
		row  agentRow
		want []string
	}{
		{
			name: "an agent carrying nothing has nothing to say",
			row:  buildNoticeRow(panel.AgentRow{Alive: true}),
			want: []string{},
		},
		{
			name: "a child without a worktree leaves its directory to its parent's row",
			row: buildPlainChildRow(panel.AgentRow{Agent: panel.Agent{
				ID:              "a2",
				ParentAgentID:   xptr.Ptr("a1"),
				WorktreeRemoved: true,
			}}),
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
			name: "a removed worktree",
			row:  buildNoticeRow(panel.AgentRow{Agent: panel.Agent{WorktreeRemoved: true}}),
			want: []string{worktreeRemovedNotice},
		},
		{
			name: "a stopped rebase",
			row:  buildNoticeRow(panel.AgentRow{Agent: panel.Agent{SequenceInProgress: true}}),
			want: []string{sequenceNotice},
		},
		{
			name: "a hidden row",
			row:  buildNoticeRow(panel.AgentRow{Agent: panel.Agent{Hidden: true}}),
			want: []string{hiddenNotice},
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
			name: "a failed create says where its worktree went",
			row: buildNoticeRow(panel.AgentRow{Agent: panel.Agent{
				State:           panel.StateFailedToCreate,
				WorktreeRemoved: true,
			}}),
			want: []string{failedToCreateNotice, worktreeRemovedNotice},
		},
		{
			name: "the marks a person would act on come first",
			row: buildNoticeRow(panel.AgentRow{
				Agent: panel.Agent{
					State:           panel.StateFailedToCleanup,
					WorktreeRemoved: true,
					Hidden:          true,
				},
				UnreadMail: true,
				FailedRun:  true,
			}),
			want: []string{failedToCleanupNotice, failedRunNotice, unreadMailNotice, worktreeRemovedNotice, hiddenNotice},
		},
		{
			name: "the person's comments read",
			row: buildNoticeRow(panel.AgentRow{PersonComments: &agentv1.CommentCounts{
				Open:     2,
				Resolved: 1,
			}}),
			want: []string{"your comments: 2 open, 1 resolved"},
		},
		{
			name: "a reviewer's comments with a reply unread come before the person's",
			row: buildNoticeRow(panel.AgentRow{
				ReviewerComments: &agentv1.CommentCounts{
					Open:   1,
					Unread: true,
				},
				PersonComments: &agentv1.CommentCounts{Resolved: 3},
			}),
			want: []string{
				"reviewer agents' comments: 1 open, 0 resolved, with something you have not read",
				"your comments: 0 open, 3 resolved",
			},
		},
		{
			name: "an archived row still counts its comments",
			row: buildNoticeRow(panel.AgentRow{
				Agent:          panel.Agent{ArchivedAt: xptr.Ptr(time.Now())},
				PersonComments: &agentv1.CommentCounts{Open: 1},
			}),
			want: []string{"your comments: 1 open, 0 resolved"},
		},
		{
			name: "an agent without a worktree still says its directory is gone",
			row: buildPlainRow(panel.AgentRow{Agent: panel.Agent{
				ID:              "a1",
				WorktreeRemoved: true,
			}}),
			want: []string{worktreeRemovedNotice},
		},
		{
			name: "an agent without a worktree still says a rebase stopped in it",
			row: buildPlainRow(panel.AgentRow{Agent: panel.Agent{
				ID:                 "a1",
				SequenceInProgress: true,
			}}),
			want: []string{sequenceNotice},
		},
		{
			name: "an agent without a worktree still counts its comments",
			row: buildPlainRow(panel.AgentRow{
				Agent:          panel.Agent{ID: "a1"},
				PersonComments: &agentv1.CommentCounts{Open: 1},
			}),
			want: []string{"your comments: 1 open, 0 resolved"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, readNoticeTexts(agentNotices(tt.row)))
		})
	}
}

// TestAgentNoticesWearTheGlyphTheRowDraws covers each notice taking its glyph and tone from the cell the row draws.
func TestAgentNoticesWearTheGlyphTheRowDraws(t *testing.T) {
	item := panel.AgentRow{
		Agent: panel.Agent{
			State:              panel.StateWaiting,
			SequenceInProgress: true,
			HeldAt:             xptr.Ptr(time.Now()),
		},
		Alive:      true,
		UnreadMail: true,
		FailedRun:  true,
	}

	line := view.Prepare(view.Snapshot{Rows: []view.Row{snapshotRow(buildNoticeRow(item), snapshotParams{})}}).Lines[0]

	want := map[string]view.Cell{
		failedRunNotice:    line.Alert,
		unreadMailNotice:   line.Mail,
		sequenceNotice:     line.Columns[0],
		heldPromptedNotice: line.Held,
	}

	notices := agentNotices(buildNoticeRow(item))
	require.Len(t, notices, len(want))

	for _, notice := range notices {
		cell, ok := want[notice.text]
		require.True(t, ok, "an unexpected notice %q", notice.text)
		assert.Equal(t, view.Spell(cell.Glyph), notice.icon, "the glyph beside %q", notice.text)
		assert.Equal(t, cell.Tone, notice.tone, "the tone beside %q", notice.text)
	}
}

// TestAgentNoticesSpeakForEveryColumnState covers every state a column draws having a sentence or being named as its baseline.
func TestAgentNoticesSpeakForEveryColumnState(t *testing.T) {
	for state := range int(view.RepoEnd) {
		repo := view.Repo(state)
		_, said := repoSaid[repo]

		assert.NotEqual(t, said, repo == view.RepoClean, "repo state %d is neither given a sentence nor its baseline", state)
	}

	for _, state := range []panel.State{panel.StateFailedToCreate, panel.StateFailedToCleanup} {
		require.Equal(t, view.StatusBroken, snapshotStatus(panel.AgentRow{Agent: panel.Agent{State: state}}), "state %q no longer draws a broken bar", state)
		assert.NotEmpty(t, failureSaid[state], "state %q draws a broken bar with no sentence", state)
	}
}

// TestAgentNoticesLeaveAStoppedRowToItsBar covers a row whose process has gone saying so with its quiet bar and grey name alone.
func TestAgentNoticesLeaveAStoppedRowToItsBar(t *testing.T) {
	stopped := buildNoticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateIdle}})
	row := snapshotRow(stopped, snapshotParams{})

	require.Equal(t, view.ProcessStopped, row.Process)
	require.Equal(t, view.GlyphQuiet, view.StatusCell(row).Glyph)
	assert.Equal(t, view.EmphasisDim, view.Prepare(view.Snapshot{Rows: []view.Row{row}}).Lines[0].Emphasis)
	assert.Empty(t, readNoticeTexts(agentNotices(stopped)))

	failed := buildNoticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate}})

	require.Equal(t, view.ProcessAlive, snapshotRow(failed, snapshotParams{}).Process, "a failed create is not a stopped process")
	assert.Contains(t, readNoticeTexts(agentNotices(failed)), failedToCreateNotice)
}

func buildNoticeRow(item panel.AgentRow) agentRow {
	return agentRow{
		item:     item,
		workflow: panel.Workflow{Worktree: panel.WorktreeConfig{Enabled: true}},
	}
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

func buildPlainChildRow(item panel.AgentRow) agentRow {
	row := buildPlainRow(item)
	row.child = true

	return row
}

func readNoticeTexts(notices []notice) []string {
	return xslice.Map(notices, func(notice notice) string {
		return notice.text
	})
}
