package ui

import (
	"strings"
	"testing"
	"time"

	"weave/internal/ui/panel"
	"weave/lib/xptr"
	"weave/lib/xslice"

	"weave/internal/git"
	"weave/internal/view"
	agentv1 "weave/proto/gen/agent/v1"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noticeRow is one agent as the list would hold it, under a workflow that cuts each agent its
// own checkout.
func noticeRow(item panel.AgentRow) agentRow {
	return agentRow{
		item:     item,
		workflow: panel.Workflow{Worktree: panel.WorktreeConfig{Enabled: true}},
	}
}

// TestNoticesSpellOutTheMarksOnTheRow covers the top of the record: every glyph the hovered row
// is carrying is said again in words.
func TestNoticesSpellOutTheMarksOnTheRow(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.width = 80
	m.listing.agents[0].Agent.WorktreeRemoved = true

	drawn := m.detailsView()

	assert.Contains(t, drawn, view.Spell(view.GlyphBroken))
	assert.Contains(t, drawn, worktreeRemovedNotice)
	assert.Contains(t, drawn, "directory", "the fields are still drawn under the notices")

	m.listing.agents[0].Agent.WorktreeRemoved = false

	assert.NotContains(t, m.detailsView(), worktreeRemovedNotice, "a mark the row is not carrying says nothing")
}

// TestArchivedNoticesClaimOnlyTheVerdict covers an archived row, whose tree is read no more:
// the review notice names the verdict as of the archiving rather than the tree as it stands.
func TestArchivedNoticesClaimOnlyTheVerdict(t *testing.T) {
	tests := []struct {
		name    string
		verdict agentv1.ReviewVerdict
		want    string
	}{
		{
			name:    "changes requested were still owed",
			verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
			want:    archivedChangesOwedNotice,
		},
		{
			name:    "an approval stood",
			verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
			want:    archivedApprovedNotice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := panel.AgentRow{
				Agent:        panel.Agent{ArchivedAt: xptr.Ptr(time.Now())},
				LocalVerdict: tt.verdict,
				LocalTree:    git.StandingMoved,
			}

			texts := xslice.Map(agentNotices(noticeRow(item)), func(n notice) string { return n.text })
			assert.Equal(t, []string{tt.want}, texts)
		})
	}
}

// TestNoticesCoverEveryOptionalMark walks the marks one at a time.
func TestNoticesCoverEveryOptionalMark(t *testing.T) {
	tests := []struct {
		name string
		item panel.AgentRow
		want string
	}{
		{"a failed create", panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate}}, failedToCreateNotice},
		{"a failed cleanup", panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCleanup}}, failedToCleanupNotice},
		{"a removed worktree", panel.AgentRow{Agent: panel.Agent{WorktreeRemoved: true}}, worktreeRemovedNotice},
		{"a stopped rebase", panel.AgentRow{Agent: panel.Agent{SequenceInProgress: true}}, sequenceNotice},
		{"a hidden row", panel.AgentRow{Agent: panel.Agent{Hidden: true}}, hiddenNotice},
		{"mail waiting", panel.AgentRow{UnreadMail: true}, unreadMailNotice},
		{"a failed script", panel.AgentRow{FailedRun: true}, failedRunNotice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notices := agentNotices(noticeRow(tt.item))

			require.Len(t, notices, 1)
			assert.Equal(t, tt.want, notices[0].text)
		})
	}

	assert.Empty(t, agentNotices(noticeRow(panel.AgentRow{Alive: true})), "an agent carrying nothing has nothing to say")
}

// TestNoticesAreOrderedByUrgency covers what a short pane gives up: the marks a person would
// act on are drawn first, so the ones dropped from the tail are the ones nobody needed spelled out.
func TestNoticesAreOrderedByUrgency(t *testing.T) {
	item := panel.AgentRow{
		Agent: panel.Agent{
			State:           panel.StateFailedToCleanup,
			WorktreeRemoved: true,
			Hidden:          true,
		},
		UnreadMail: true,
		FailedRun:  true,
	}

	texts := xslice.Map(agentNotices(noticeRow(item)), func(n notice) string {
		return n.text
	})

	assert.Equal(t, []string{
		failedToCleanupNotice,
		failedRunNotice,
		unreadMailNotice,
		worktreeRemovedNotice,
		hiddenNotice,
	}, texts)
}

// TestAFailedCreateSaysWhereItsWorktreeWent covers the pair a failed setup draws: the failure
// itself, and the directory that was made for it and taken away again on the way out.
func TestAFailedCreateSaysWhereItsWorktreeWent(t *testing.T) {
	item := panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate, WorktreeRemoved: true}}

	notices := agentNotices(noticeRow(item))

	require.Len(t, notices, 2)
	assert.Equal(t, failedToCreateNotice, notices[0].text)
	assert.Equal(t, worktreeRemovedNotice, notices[1].text)
}

// TestNoticesWearTheGlyphTheRowDraws is the claim the record rests on: every notice about a mark
// takes its glyph and its tone from the very cell the rebuilt row draws, so the two cannot say
// different things about one fact.
func TestNoticesWearTheGlyphTheRowDraws(t *testing.T) {
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

// TestNoticesKeepTheRecordsHeight covers the share the record is allowed: the notices take their
// lines from the fields rather than from the list above, so the block is the height it always was.
func TestNoticesKeepTheRecordsHeight(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60

	plain := strings.Count(m.detailsView(), "\n")

	m.listing.agents[0].Agent.WorktreeRemoved = true
	m.listing.agents[0].UnreadMail = true
	m.listing.agents[0].FailedRun = true

	flagged := m.detailsView()

	assert.Equal(t, plain, strings.Count(flagged, "\n"))
	assert.Contains(t, flagged, worktreeRemovedNotice)
	assert.Contains(t, flagged, "name", "a field survives however many marks the row carries")
}

// TestNoticesGiveUpBeforeTheFields covers a pane too short for everything: the notices that do
// not fit are dropped, and the fields keep a line.
func TestNoticesGiveUpBeforeTheFields(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 13, 40
	m.listing.agents[0].Agent.WorktreeRemoved = true
	m.listing.agents[0].UnreadMail = true
	m.listing.agents[0].FailedRun = true

	drawn := m.detailsView()

	assert.Equal(t, m.detailsLines()+detailsChromeLines, strings.Count(drawn, "\n"))
	assert.NotContains(t, drawn, failedRunNotice, "the last mark is given up rather than the fields")
	assert.Contains(t, drawn, "name", "a field is still drawn")
}

// TestNoticesCutTheFirstOneShort covers a pane too short for even one whole sentence, where the
// notice is drawn as far as it goes and says so.
func TestNoticesCutTheFirstOneShort(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 13, 24
	m.listing.agents[0].FailedRun = true

	drawn := m.detailsView()

	assert.Contains(t, drawn, "…")
	assert.Contains(t, drawn, "a script weave")
	assert.NotContains(t, drawn, "log unread")
}

// TestNoticesStayInsideTheColumn covers the cut line.
func TestNoticesStayInsideTheColumn(t *testing.T) {
	for width := 16; width <= 120; width++ {
		m := detailsModel(&recordingTmux{})
		m.height, m.width = 16, width
		m.listing.agents[0].FailedRun = true
		m.listing.agents[0].UnreadMail = true

		for _, line := range strings.Split(m.detailsView(), "\n") {
			assert.LessOrEqual(t, lipgloss.Width(line), width, "a notice ran past the column at width %d", width)
		}
	}
}

// TestNoticesLeaveRoomToCountTheFields covers what the record keeps back for the fields: enough
// for a field and the line saying how many more there are.
func TestNoticesLeaveRoomToCountTheFields(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 20, 40
	m.listing.agents[0].FailedRun = true

	drawn := m.detailsView()

	assert.Contains(t, drawn, "name")
	assert.Contains(t, drawn, "more", "the record still says how many fields it left out")
}

// TestNoticesAlignAndFitTheColumn covers the layout the block depends on: every glyph the record
// draws takes one column, so an Octicon notice, a bar notice and an iconless one have to put
// their sentences in the same place as each other and as their own wrapped continuations.
func TestNoticesAlignAndFitTheColumn(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60

	// A bar notice and an Octicon notice, stacked.
	m.listing.agents[0].Agent.State = panel.StateFailedToCleanup
	m.listing.agents[0].Agent.WorktreeRemoved = true

	drawn := m.detailsNotices(m.hoveredDetails(), m.detailsLines()-3)
	require.Len(t, drawn, 2, "a bar and an octicon")

	wants := []string{failedToCleanupNotice, worktreeRemovedNotice}
	starts := make([]int, 0, len(drawn))

	for i, line := range drawn {
		bare := ansi.Strip(line)
		at := strings.Index(bare, wants[i])
		require.GreaterOrEqual(t, at, 0, "notice %d does not say %q", i, wants[i])
		starts = append(starts, lipgloss.Width(bare[:at]))
	}

	assert.Equal(t, []int{starts[0], starts[0]}, starts,
		"every notice puts its sentence in the same column")

	// A wrapped notice's continuation lines have to land in that same column.
	long := detailsModel(&recordingTmux{})
	long.height, long.width = 40, 34
	long.listing.agents[0].Agent.HeldAt = xptr.Ptr(time.Unix(0, 0))

	wrapped := long.detailsNotices(long.hoveredDetails(), long.detailsLines()-3)
	require.Greater(t, len(wrapped), 1, "the pause sentence has to wrap at this width")

	for _, line := range wrapped[1:] {
		bare := ansi.Strip(line)
		assert.Equal(t, starts[0], lipgloss.Width(bare)-lipgloss.Width(strings.TrimLeft(bare, " ")),
			"a continuation line left the sentence column")
	}

	for width := 20; width <= 120; width++ {
		m := detailsModel(&recordingTmux{})
		m.height, m.width = 16, width
		m.listing.agents[0].UnreadMail = true
		m.listing.agents[0].FailedRun = true

		for _, line := range m.detailsNotices(m.hoveredDetails(), m.detailsLines()-3) {
			assert.LessOrEqual(t, lipgloss.Width(ansi.Strip(line)), width,
				"a notice ran past the column at width %d", width)
		}
	}
}

// TestEveryRepoStateIsSpokenForOrSilent walks the repo column.
func TestEveryRepoStateIsSpokenForOrSilent(t *testing.T) {
	// RepoClean is the baseline: a checkout an agent can work in is not news.
	silent := map[view.Repo]bool{view.RepoClean: true}

	for state := range int(view.RepoEnd) {
		repo := view.Repo(state)
		_, said := repoSaid[repo]

		assert.NotEqual(t, said, silent[repo],
			"repo state %d is neither given a sentence nor named as deliberately silent", state)
	}
}

// TestEveryReviewStateIsSpokenForOrSilent walks the review column.
func TestEveryReviewStateIsSpokenForOrSilent(t *testing.T) {
	// ReviewNone has had no reviewer, which the column itself draws at its baseline.
	silent := map[view.Review]bool{view.ReviewNone: true}

	for state := range int(view.ReviewEnd) {
		review := view.Review(state)
		_, said := reviewSaid[review]

		assert.NotEqual(t, said, silent[review],
			"review state %d is neither given a sentence nor named as deliberately silent", state)
	}
}

// TestEveryStateTheBarDrawsBrokenHasASentence covers the one status the record speaks for.
func TestEveryStateTheBarDrawsBrokenHasASentence(t *testing.T) {
	for _, state := range []panel.State{panel.StateFailedToCreate, panel.StateFailedToCleanup} {
		item := panel.AgentRow{Agent: panel.Agent{State: state}}

		require.Equal(t, view.StatusBroken, snapshotStatus(item), "state %q no longer draws a broken bar", state)
		assert.NotEmpty(t, failureSaid[state], "state %q draws a broken bar with no sentence", state)
	}
}

// TestAStoppedRowKeepsItsBarAndSaysNothing covers a row whose process has gone: its bar is the
// quiet one, its name greys, and the record has nothing to add.
func TestAStoppedRowKeepsItsBarAndSaysNothing(t *testing.T) {
	stopped := panel.AgentRow{Agent: panel.Agent{State: panel.StateIdle}}
	row := snapshotRow(noticeRow(stopped), snapshotParams{})

	require.Equal(t, view.ProcessStopped, row.Process, "an agent whose pane has gone reads as stopped")
	require.Equal(t, view.GlyphQuiet, view.StatusCell(row).Glyph, "and its bar is the quiet pipe")
	assert.Equal(t, view.EmphasisDim, view.Prepare(view.Snapshot{Rows: []view.Row{row}}).Lines[0].Emphasis,
		"its name is what says the process has gone")
	assert.Empty(t, noticeTexts(agentNotices(noticeRow(stopped))), "the record says nothing about it")

	failed := panel.AgentRow{Agent: panel.Agent{State: panel.StateFailedToCreate}}

	require.Equal(t, view.ProcessAlive, snapshotRow(noticeRow(failed), snapshotParams{}).Process,
		"a failed create is not a stopped process")
	assert.Contains(t, noticeTexts(agentNotices(noticeRow(failed))), failedToCreateNotice)
}

// TestNoticesReportAPlainWorkflowsDirectory covers the agent weave cut no worktree for: nothing
// else in the panel reports the directory it was handed or the review of its work, so its own
// record has to.
func TestNoticesReportAPlainWorkflowsDirectory(t *testing.T) {
	plain := func(agent panel.Agent) agentRow {
		return agentRow{
			workflow: panel.Workflow{ID: "w1", Name: "weave"},
			item:     panel.AgentRow{Alive: true, Agent: agent},
		}
	}

	gone := plain(panel.Agent{
		ID:              "a1",
		Name:            "worker",
		Cwd:             "/tmp/somewhere",
		WorktreeRemoved: true,
	})
	rebasing := plain(panel.Agent{
		ID:                 "a1",
		Name:               "worker",
		Cwd:                "/tmp/somewhere",
		SequenceInProgress: true,
	})

	reviewed := plain(panel.Agent{
		ID:   "a1",
		Name: "worker",
		Cwd:  "/tmp/somewhere",
	})
	reviewed.item.LocalVerdict = agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED

	assert.Equal(t, []string{worktreeRemovedNotice}, noticeTexts(agentNotices(gone)))
	assert.Equal(t, []string{sequenceNotice}, noticeTexts(agentNotices(rebasing)))
	assert.Equal(t, []string{localApprovedNotice}, noticeTexts(agentNotices(reviewed)))

	child := plain(panel.Agent{
		ID:              "a2",
		Name:            "reviewer",
		Cwd:             "/tmp/somewhere",
		WorktreeRemoved: true,
	})
	child.child = true
	child.item.Agent.ParentAgentID = xptr.Ptr("a1")

	assert.Empty(t, noticeTexts(agentNotices(child)), "its parent's row already says it")
}

// TestReviewStateNoticesSayWhoseTurnItIs covers the record saying whose move is next in each review state.
func TestReviewStateNoticesSayWhoseTurnItIs(t *testing.T) {
	tests := []struct {
		name string
		item panel.AgentRow
		want string
	}{
		{
			name: "never reviewed says nothing",
			item: panel.AgentRow{},
		},
		{
			name: "a review released without a conclusion says nothing",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED},
		},
		{
			name: "a tree that moved with nothing concluded on it says nothing",
			item: panel.AgentRow{LocalTree: git.StandingMoved},
		},
		{
			name: "approved on this tree",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED},
			want: localApprovedNotice,
		},
		{
			name: "approved, tree moved since",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED, LocalTree: git.StandingMoved},
			want: localApprovedMovedNotice,
		},
		{
			name: "changes requested, agent still owes fixes",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED},
			want: localChangesOwedNotice,
		},
		{
			name: "changes requested, agent pushed since",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED, LocalTree: git.StandingMoved},
			want: localChangesPushedNotice,
		},
		{
			name: "approved and landed",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED, LocalTree: git.StandingLanded},
			want: localApprovedLandedNotice,
		},
		{
			name: "approved and landed, new work since",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED, LocalTree: git.StandingLandedMoved},
			want: localApprovedLandedMovedNotice,
		},
		{
			name: "changes requested, landed as it was",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED, LocalTree: git.StandingLanded},
			want: localChangesLandedNotice,
		},
		{
			name: "changes requested, landed with new work",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED, LocalTree: git.StandingLandedMoved},
			want: localChangesLandedMovedNotice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notices := agentNotices(noticeRow(tt.item))
			if tt.want == "" {
				require.Empty(t, notices)

				return
			}

			require.Len(t, notices, 1)
			assert.Equal(t, tt.want, notices[0].text)
		})
	}
}

// noticeTexts is what a row's notices say, without their icons.
func noticeTexts(notices []notice) []string {
	said := xslice.Map(notices, func(n notice) string {
		return n.text
	})

	return said
}
