package ui

import (
	"testing"
	"time"

	"weave/internal/git"
	"weave/internal/ui/panel"
	"weave/internal/view"
	"weave/lib/xptr"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			row:  noticeRow(panel.AgentRow{Alive: true}),
			want: []string{},
		},
		{
			name: "a failed create",
			row:  noticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate}}),
			want: []string{failedToCreateNotice},
		},
		{
			name: "a failed cleanup",
			row:  noticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCleanup}}),
			want: []string{failedToCleanupNotice},
		},
		{
			name: "a removed worktree",
			row:  noticeRow(panel.AgentRow{Agent: panel.Agent{WorktreeRemoved: true}}),
			want: []string{worktreeRemovedNotice},
		},
		{
			name: "a stopped rebase",
			row:  noticeRow(panel.AgentRow{Agent: panel.Agent{SequenceInProgress: true}}),
			want: []string{sequenceNotice},
		},
		{
			name: "a hidden row",
			row:  noticeRow(panel.AgentRow{Agent: panel.Agent{Hidden: true}}),
			want: []string{hiddenNotice},
		},
		{
			name: "mail waiting",
			row:  noticeRow(panel.AgentRow{UnreadMail: true}),
			want: []string{unreadMailNotice},
		},
		{
			name: "a failed script",
			row:  noticeRow(panel.AgentRow{FailedRun: true}),
			want: []string{failedRunNotice},
		},
		{
			name: "a failed create says where its worktree went",
			row: noticeRow(panel.AgentRow{Agent: panel.Agent{
				State:           panel.StateFailedToCreate,
				WorktreeRemoved: true,
			}}),
			want: []string{failedToCreateNotice, worktreeRemovedNotice},
		},
		{
			name: "the marks a person would act on come first",
			row: noticeRow(panel.AgentRow{
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
			name: "a review released without a conclusion says nothing",
			row:  noticeRow(panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED}),
			want: []string{},
		},
		{
			name: "a tree that moved with nothing concluded on it says nothing",
			row:  noticeRow(panel.AgentRow{LocalTree: git.StandingMoved}),
			want: []string{},
		},
		{
			name: "approved on this tree",
			row:  noticeRow(panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED}),
			want: []string{localApprovedNotice},
		},
		{
			name: "approved, tree moved since",
			row: noticeRow(panel.AgentRow{
				LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
				LocalTree:    git.StandingMoved,
			}),
			want: []string{localApprovedMovedNotice},
		},
		{
			name: "changes requested, agent still owes fixes",
			row:  noticeRow(panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED}),
			want: []string{localChangesOwedNotice},
		},
		{
			name: "changes requested, agent pushed since",
			row: noticeRow(panel.AgentRow{
				LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
				LocalTree:    git.StandingMoved,
			}),
			want: []string{localChangesPushedNotice},
		},
		{
			name: "approved and landed",
			row: noticeRow(panel.AgentRow{
				LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
				LocalTree:    git.StandingLanded,
			}),
			want: []string{localApprovedLandedNotice},
		},
		{
			name: "approved and landed, new work since",
			row: noticeRow(panel.AgentRow{
				LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
				LocalTree:    git.StandingLandedMoved,
			}),
			want: []string{localApprovedLandedMovedNotice},
		},
		{
			name: "changes requested, landed as it was",
			row: noticeRow(panel.AgentRow{
				LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
				LocalTree:    git.StandingLanded,
			}),
			want: []string{localChangesLandedNotice},
		},
		{
			name: "changes requested, landed with new work",
			row: noticeRow(panel.AgentRow{
				LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
				LocalTree:    git.StandingLandedMoved,
			}),
			want: []string{localChangesLandedMovedNotice},
		},
		{
			name: "an archived row with changes still owed names the verdict as of the archiving",
			row: noticeRow(panel.AgentRow{
				Agent:        panel.Agent{ArchivedAt: xptr.Ptr(time.Now())},
				LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
				LocalTree:    git.StandingMoved,
			}),
			want: []string{archivedChangesOwedNotice},
		},
		{
			name: "an archived row with an approval standing names the verdict as of the archiving",
			row: noticeRow(panel.AgentRow{
				Agent:        panel.Agent{ArchivedAt: xptr.Ptr(time.Now())},
				LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
				LocalTree:    git.StandingMoved,
			}),
			want: []string{archivedApprovedNotice},
		},
		{
			name: "an agent without a worktree still says its directory is gone",
			row: plainRow(panel.AgentRow{Agent: panel.Agent{
				ID:              "a1",
				WorktreeRemoved: true,
			}}),
			want: []string{worktreeRemovedNotice},
		},
		{
			name: "an agent without a worktree still says a rebase stopped in it",
			row: plainRow(panel.AgentRow{Agent: panel.Agent{
				ID:                 "a1",
				SequenceInProgress: true,
			}}),
			want: []string{sequenceNotice},
		},
		{
			name: "an agent without a worktree still says how its review stands",
			row: plainRow(panel.AgentRow{
				Agent:        panel.Agent{ID: "a1"},
				LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
			}),
			want: []string{localApprovedNotice},
		},
		{
			name: "a child without a worktree leaves its directory to its parent's row",
			row: plainChildRow(panel.AgentRow{Agent: panel.Agent{
				ID:              "a2",
				ParentAgentID:   xptr.Ptr("a1"),
				WorktreeRemoved: true,
			}}),
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, noticeTexts(agentNotices(tt.row)))
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
		Alive:        true,
		UnreadMail:   true,
		FailedRun:    true,
		LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
		LocalTree:    git.StandingMoved,
	}

	line := view.Prepare(view.Snapshot{Rows: []view.Row{snapshotRow(noticeRow(item), snapshotParams{})}}).Lines[0]

	want := map[string]view.Cell{
		failedRunNotice:          line.Alert,
		unreadMailNotice:         line.Mail,
		sequenceNotice:           line.Columns[0],
		heldPromptedNotice:       line.Held,
		localChangesPushedNotice: line.Columns[1],
	}

	notices := agentNotices(noticeRow(item))
	require.Len(t, notices, len(want))

	for _, n := range notices {
		cell, ok := want[n.text]
		require.True(t, ok, "an unexpected notice %q", n.text)
		assert.Equal(t, view.Spell(cell.Glyph), n.icon, "the glyph beside %q", n.text)
		assert.Equal(t, cell.Tone, n.tone, "the tone beside %q", n.text)
	}
}

// TestAgentNoticesSpeakForEveryColumnState covers every state a column draws having a sentence or being named as its baseline.
func TestAgentNoticesSpeakForEveryColumnState(t *testing.T) {
	for state := range int(view.RepoEnd) {
		repo := view.Repo(state)
		_, said := repoSaid[repo]

		assert.NotEqual(t, said, repo == view.RepoClean, "repo state %d is neither given a sentence nor its baseline", state)
	}

	for state := range int(view.ReviewEnd) {
		review := view.Review(state)
		_, said := reviewSaid[review]

		assert.NotEqual(t, said, review == view.ReviewNone, "review state %d is neither given a sentence nor its baseline", state)
	}

	for _, state := range []panel.State{panel.StateFailedToCreate, panel.StateFailedToCleanup} {
		require.Equal(t, view.StatusBroken, snapshotStatus(panel.AgentRow{Agent: panel.Agent{State: state}}), "state %q no longer draws a broken bar", state)
		assert.NotEmpty(t, failureSaid[state], "state %q draws a broken bar with no sentence", state)
	}
}

// TestAgentNoticesLeaveAStoppedRowToItsBar covers a row whose process has gone saying so with its quiet bar and grey name alone.
func TestAgentNoticesLeaveAStoppedRowToItsBar(t *testing.T) {
	stopped := noticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateIdle}})
	row := snapshotRow(stopped, snapshotParams{})

	require.Equal(t, view.ProcessStopped, row.Process)
	require.Equal(t, view.GlyphQuiet, view.StatusCell(row).Glyph)
	assert.Equal(t, view.EmphasisDim, view.Prepare(view.Snapshot{Rows: []view.Row{row}}).Lines[0].Emphasis)
	assert.Empty(t, noticeTexts(agentNotices(stopped)))

	failed := noticeRow(panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate}})

	require.Equal(t, view.ProcessAlive, snapshotRow(failed, snapshotParams{}).Process, "a failed create is not a stopped process")
	assert.Contains(t, noticeTexts(agentNotices(failed)), failedToCreateNotice)
}

func noticeRow(item panel.AgentRow) agentRow {
	return agentRow{
		item:     item,
		workflow: panel.Workflow{Worktree: panel.WorktreeConfig{Enabled: true}},
	}
}

func plainRow(item panel.AgentRow) agentRow {
	item.Alive = true

	return agentRow{
		item: item,
		workflow: panel.Workflow{
			ID:   "w1",
			Name: "weave",
		},
	}
}

func plainChildRow(item panel.AgentRow) agentRow {
	row := plainRow(item)
	row.child = true

	return row
}

func noticeTexts(notices []notice) []string {
	return xslice.Map(notices, func(n notice) string {
		return n.text
	})
}
