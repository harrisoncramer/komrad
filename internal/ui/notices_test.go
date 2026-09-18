package ui

import (
	"strings"
	"testing"
	"time"

	"weave/lib/xptr"

	agentstore "weave/internal/store/agent"
	"weave/internal/view"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoticesSpellOutTheMarksOnTheRow covers the top of the record: every glyph the hovered row
// is carrying is said again in words.
func TestNoticesSpellOutTheMarksOnTheRow(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.width = 80
	m.listing.agents[0].agent.WorktreeRemoved = true

	drawn := m.detailsView()

	assert.Contains(t, drawn, view.Spell(view.GlyphBroken))
	assert.Contains(t, drawn, worktreeRemovedNotice)
	assert.Contains(t, drawn, "directory", "the fields are still drawn under the notices")

	m.listing.agents[0].agent.WorktreeRemoved = false

	assert.NotContains(t, m.detailsView(), worktreeRemovedNotice, "a mark the row is not carrying says nothing")
}

// TestNoticesCoverEveryOptionalMark walks the marks one at a time.
func TestNoticesCoverEveryOptionalMark(t *testing.T) {
	tests := []struct {
		name string
		item agentItem
		want string
	}{
		{"a failed create", agentItem{agent: agentstore.Agent{State: agentstore.StateFailedToCreate}}, failedToCreateNotice},
		{"a failed cleanup", agentItem{agent: agentstore.Agent{State: agentstore.StateFailedToCleanup}}, failedToCleanupNotice},
		{"a removed worktree", agentItem{agent: agentstore.Agent{WorktreeRemoved: true}}, worktreeRemovedNotice},
		{"a stopped rebase", agentItem{agent: agentstore.Agent{SequenceInProgress: true}}, sequenceNotice},
		{"a hidden row", agentItem{agent: agentstore.Agent{Hidden: true}}, hiddenNotice},
		{"mail waiting", agentItem{unreadMail: true}, unreadMailNotice},
		{"a failed script", agentItem{failedRun: true}, failedRunNotice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notices := agentNotices(tt.item)

			require.Len(t, notices, 1)
			assert.Equal(t, tt.want, notices[0].text)
		})
	}

	assert.Empty(t, agentNotices(agentItem{alive: true}), "an agent carrying nothing has nothing to say")
}

// TestNoticesAreOrderedByUrgency covers what a short pane gives up: the marks a person would
// act on are drawn first, so the ones dropped from the tail are the ones nobody needed spelled out.
func TestNoticesAreOrderedByUrgency(t *testing.T) {
	item := agentItem{
		agent: agentstore.Agent{
			State:           agentstore.StateFailedToCleanup,
			WorktreeRemoved: true,
			Hidden:          true,
		},
		unreadMail: true,
		failedRun:  true,
	}

	texts := make([]string, 0, len(agentNotices(item)))
	for _, n := range agentNotices(item) {
		texts = append(texts, n.text)
	}

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
	item := agentItem{agent: agentstore.Agent{State: agentstore.StateFailedToCreate, WorktreeRemoved: true}}

	notices := agentNotices(item)

	require.Len(t, notices, 2)
	assert.Equal(t, failedToCreateNotice, notices[0].text)
	assert.Equal(t, worktreeRemovedNotice, notices[1].text)
}

// TestNoticesWearTheGlyphTheRowDraws is the claim the record rests on: every notice about a mark
// takes its glyph and its tone from the very cell the rebuilt row draws, so the two cannot say
// different things about one fact.
func TestNoticesWearTheGlyphTheRowDraws(t *testing.T) {
	item := agentItem{
		agent: agentstore.Agent{
			State:              agentstore.StateWaiting,
			SequenceInProgress: true,
			HeldAt:             xptr.Ptr(time.Now()),
		},
		alive:         true,
		unreadMail:    true,
		failedRun:     true,
		inLocalReview: true,
	}

	line := view.Prepare(view.Snapshot{Rows: []view.Row{snapshotRow(agentRow{item: item}, snapshotParams{})}}).Lines[0]

	want := map[string]view.Cell{
		failedRunNotice:     line.Alert,
		unreadMailNotice:    line.Mail,
		sequenceNotice:      line.Repo,
		heldPromptedNotice:  line.Held,
		inLocalReviewNotice: line.Review,
	}

	notices := agentNotices(item)
	require.Len(t, notices, len(want))

	for _, n := range notices {
		cell, ok := want[n.text]
		require.True(t, ok, "an unexpected notice %q", n.text)
		assert.Equal(t, view.Spell(cell.Glyph), n.icon, "the glyph beside %q", n.text)
		assert.Equal(t, cell.Tone, n.tone, "the tone beside %q", n.text)
	}
}

// TestUnviewedAndHiddenCarryNoGlyph is why one of the two was dropped and the other kept: the
// rebuilt row carries neither fact as a mark, and the row the record is about is the row under
// the cursor, whose own colour beats both the brightening and the greying.
func TestUnviewedAndHiddenCarryNoGlyph(t *testing.T) {
	bright := view.Prepare(view.Snapshot{
		Rows:    []view.Row{{Key: "a", Unviewed: true, Dim: true}},
		Focused: "a",
	}).Lines[0]

	assert.Equal(t, view.EmphasisFocused, bright.Emphasis, "the hovered row says neither")

	said := noticeTexts(agentNotices(agentItem{unviewed: true, agent: agentstore.Agent{Hidden: true}}))

	assert.Equal(t, []string{hiddenNotice}, said, "hidden is durable, unviewed goes on a second's dwell")
}

// TestArchivedNoticesAreOnlyTheFailures covers the archive, whose rows draw one mark of their
// own.
func TestArchivedNoticesAreOnlyTheFailures(t *testing.T) {
	agent := agentstore.Agent{
		State:      agentstore.StateFailedToCleanup,
		ScheduleID: xptr.Ptr("s1"),
		Hidden:     true,
	}

	require.Len(t, failureNotices(agent.State), 1)
	assert.Equal(t, failedToCleanupNotice, failureNotices(agent.State)[0].text)
	assert.Empty(t, failureNotices(agentstore.StateIdle))
}

// TestNoticesKeepTheRecordsHeight covers the share the record is allowed: the notices take their
// lines from the fields rather than from the list above, so the block is the height it always was.
func TestNoticesKeepTheRecordsHeight(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60

	plain := strings.Count(m.detailsView(), "\n")

	m.listing.agents[0].agent.WorktreeRemoved = true
	m.listing.agents[0].unreadMail = true
	m.listing.agents[0].failedRun = true

	flagged := m.detailsView()

	assert.Equal(t, plain, strings.Count(flagged, "\n"))
	assert.Contains(t, flagged, worktreeRemovedNotice)
	assert.Contains(t, flagged, "name", "a field survives however many marks the row carries")
}

// TestNoticesGiveUpBeforeTheFields covers a pane too short for everything: the notices that do
// not fit are dropped, and the fields keep a line.
func TestNoticesGiveUpBeforeTheFields(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 12, 40
	m.listing.agents[0].agent.WorktreeRemoved = true
	m.listing.agents[0].unreadMail = true
	m.listing.agents[0].failedRun = true

	drawn := m.detailsView()

	assert.Equal(t, m.detailsLines()+detailsChromeLines, strings.Count(drawn, "\n"))
	assert.NotContains(t, drawn, failedRunNotice, "the last mark is given up rather than the fields")
	assert.Contains(t, drawn, "name", "a field is still drawn")
}

// TestNoticesCutTheFirstOneShort covers a pane too short for even one whole sentence, where the
// notice is drawn as far as it goes and says so.
func TestNoticesCutTheFirstOneShort(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 12, 24
	m.listing.agents[0].failedRun = true

	drawn := m.detailsView()

	assert.Contains(t, drawn, "…")
	assert.Contains(t, drawn, "a script weave")
	assert.NotContains(t, drawn, "log unread")
}

// TestNoticesStayInsideTheColumn covers the cut line, which is the one place the record draws
// prose it does not clip: the ellipsis has to sit inside the column rather than a cell past it.
func TestNoticesStayInsideTheColumn(t *testing.T) {
	for width := 16; width <= 120; width++ {
		m := detailsModel(&recordingTmux{})
		m.height, m.width = 16, width
		m.listing.agents[0].failedRun = true
		m.listing.agents[0].unreadMail = true

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
	m.listing.agents[0].failedRun = true

	drawn := m.detailsView()

	assert.Contains(t, drawn, "name")
	assert.Contains(t, drawn, "more", "the record still says how many fields it left out")
}

// octiconSpellings is every glyph the record can draw that the terminal gives two columns and
// lipgloss gives one.
func octiconSpellings() []string {
	spelled := []string{}
	for _, glyph := range []view.Glyph{view.GlyphMail, view.GlyphAlert, view.GlyphBroken, view.GlyphPaused, view.GlyphRepo, view.GlyphReview, view.GlyphChanges, view.GlyphApproved} {
		spelled = append(spelled, view.Spell(glyph))
	}

	return spelled
}

// noticeColumns is how many columns a drawn stretch really takes, correcting lipgloss for every
// Octicon in it.
func noticeColumns(text string) int {
	columns := lipgloss.Width(text)
	for _, icon := range octiconSpellings() {
		columns += strings.Count(text, icon) * (view.GlyphColumns(view.GlyphMail) - 1)
	}

	return columns
}

// TestNoticesAlignAgainstTheRowsOwnWidths covers what lipgloss cannot see: internal/view gives an
// Octicon two columns and a bar one, so the record has to reserve the glyph column rather than
// assume every icon is one wide, and no notice may run past the pane.
func TestNoticesAlignAgainstTheRowsOwnWidths(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60

	// A bar notice, an Octicon notice and an iconless one, stacked.
	m.listing.agents[0].agent.State = agentstore.StateFailedToCleanup
	m.listing.agents[0].agent.WorktreeRemoved = true
	m.listing.agents[0].localRounds = 2

	drawn := m.detailsNotices(m.hoveredDetails(), m.detailsLines()-3)
	require.Len(t, drawn, 3, "a bar, an octicon and a blank")

	wants := []string{failedToCleanupNotice, worktreeRemovedNotice, "reviewed 2 times"}
	starts := make([]int, 0, len(drawn))

	for i, line := range drawn {
		bare := ansi.Strip(line)
		at := strings.Index(bare, wants[i])
		require.GreaterOrEqual(t, at, 0, "notice %d does not say %q", i, wants[i])
		starts = append(starts, noticeColumns(bare[:at]))
	}

	assert.Equal(t, []int{starts[0], starts[0], starts[0]}, starts,
		"every notice puts its sentence in the same column")

	for width := 20; width <= 120; width++ {
		m := detailsModel(&recordingTmux{})
		m.height, m.width = 16, width
		m.listing.agents[0].unreadMail = true
		m.listing.agents[0].failedRun = true

		for _, line := range m.detailsNotices(m.hoveredDetails(), m.detailsLines()-3) {
			assert.LessOrEqual(t, noticeColumns(ansi.Strip(line)), width,
				"a notice ran past the column at width %d", width)
		}
	}
}

// Explainer: the notice tables gave up the compile-time answer-for-it that an exhaustive switch
// buys, so these keep it where it is actually wanted. A new column state draws a glyph on every
// row that has it, and a table that has never heard of it stays silent, which is the record
// disagreeing with the row about a mark: the defect this whole branch exists to close, arriving
// with the suite green. Requiring every state to be either spoken for or named as deliberately
// silent fails on the day somebody adds one. The bar is the exception: its sentences key on the
// agent state behind it rather than on the bar, and agentstore.State is a string enum mirroring a
// Postgres one, so there is no sentinel to walk. What forces an answer for an eighth state there
// is snapshotStatus, whose switch over State carries no default.

// TestEveryRepoStateIsSpokenForOrSilent walks the repo column.
func TestEveryRepoStateIsSpokenForOrSilent(t *testing.T) {
	// RepoClean is the baseline: a checkout an agent can work in is not news.
	silent := map[view.Repo]bool{view.RepoClean: true}

	for state := range view.RepoCount {
		repo := view.Repo(state)
		_, said := repoSaid[repo]

		assert.NotEqual(t, said, silent[repo],
			"repo state %d is neither given a sentence nor named as deliberately silent", state)
	}
}

// TestEveryReviewStateIsSpokenForOrSilent walks the review column.
func TestEveryReviewStateIsSpokenForOrSilent(t *testing.T) {
	// ReviewNone has had no reviewer, and ReviewCommented is notes without a conclusion, which
	// the column itself draws at its baseline.
	silent := map[view.Review]bool{view.ReviewNone: true, view.ReviewCommented: true}

	for state := range view.ReviewCount {
		review := view.Review(state)
		_, said := reviewSaid[review]

		assert.NotEqual(t, said, silent[review],
			"review state %d is neither given a sentence nor named as deliberately silent", state)
	}
}

// TestEveryStateTheBarDrawsBrokenHasASentence covers the one status the record speaks for.
func TestEveryStateTheBarDrawsBrokenHasASentence(t *testing.T) {
	for _, state := range []agentstore.State{agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup} {
		item := agentItem{agent: agentstore.Agent{State: state}}

		require.Equal(t, view.StatusBroken, snapshotStatus(item), "state %q no longer draws a broken bar", state)
		assert.NotEmpty(t, failureSaid[state], "state %q draws a broken bar with no sentence", state)
	}
}
