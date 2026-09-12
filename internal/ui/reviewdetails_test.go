package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewDrawsTheAgentRecord covers the diff carrying the same record the agents list
// draws.
func TestReviewDrawsTheAgentRecord(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	view := ansi.Strip(m.reviewView())

	assert.Contains(t, view, "› name")
	assert.Contains(t, view, "agent_id")

	held := m.hoveredDetails()
	bottom := ansi.Strip(m.setDetailsCursor(held, len(held.fields)-1).reviewView())

	assert.Contains(t, bottom, "base", "the commit the diff was taken from")
	assert.Contains(t, bottom, "snapshot", "the commit the diff on screen was frozen as")
}

// TestReviewRecordNamesWhatTheDiffIsAgainst covers the record naming the two commits the diff
// was taken between, which is what the agent can run for itself.
func TestReviewRecordNamesWhatTheDiffIsAgainst(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	require.NotNil(t, m.review.view)
	require.NotNil(t, m.review.round)

	assert.Equal(t, shortSHA(m.review.round.GetBaseSha()), fieldValue(t, m.reviewDetails(), "base"))
	assert.Equal(t, shortSHA(m.review.round.GetSnapshotSha()), fieldValue(t, m.reviewDetails(), "snapshot"))

	for _, field := range m.reviewDetails().fields {
		assert.NotEqual(t, "round", field.label, "the reader is never shown weave's numbering")
	}
}

// TestReviewRecordTakesTheKeys covers the whole path a keypress takes.
func TestReviewRecordTakesTheKeys(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	require.True(t, m.isDetailsScreen(), "the review draws a record")
	require.False(t, m.isDetailsFocused())

	next, _ := m.Update(keyMsg("ctrl+t"))
	focused := next.(model)
	require.True(t, focused.isDetailsFocused())

	// The keys are on the record, so a move walks its fields rather than the diff.
	cursor := focused.review.cursor
	next, _ = focused.Update(keyMsg("j"))
	moved := next.(model)

	assert.Equal(t, cursor, moved.review.cursor, "the diff stayed where it was")
	assert.Equal(t, 1, moved.details.cursor)

	next, _ = moved.Update(keyMsg("ctrl+t"))
	assert.False(t, next.(model).isDetailsFocused(), "the same key gives the keys back")
}

// TestReviewRecordCopiesAField covers the copy key reaching the record on this screen too.
func TestReviewRecordCopiesAField(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.Update(keyMsg("ctrl+t"))
	focused := next.(model)

	copied, cmd := focused.Update(keyMsg("y"))
	require.NotNil(t, cmd)
	assert.NotZero(t, copied.(model).details.flashed)
}

// TestReviewDimsTheCursorWhileTheRecordHasTheKeys covers the cursor row saying where the keys
// are.
func TestReviewDimsTheCursorWhileTheRecordHasTheKeys(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	live := reviewFrameRaw(t, m)

	respawns := len(tmux.respawns)
	tmux.sentKeys = nil

	next, _ := m.Update(keyMsg("ctrl+t"))
	focused := next.(model)
	require.True(t, focused.isDetailsFocused())

	dim := reviewFrameRaw(t, focused)
	assert.NotEqual(t, live, dim, "the cursor row is painted differently while the keys are away")
	assert.Len(t, tmux.respawns, respawns, "nothing is respawned")
	assert.Empty(t, tmux.sentKeys, "and nothing is tapped to re-read a file")
	assert.Equal(t, m.height, reviewFrameRows(t, focused))

	next, _ = focused.Update(keyMsg("ctrl+t"))
	back := next.(model)
	require.False(t, back.isDetailsFocused())

	assert.Equal(t, live, reviewFrameRaw(t, back), "and painted back when the keys come back")
}

// TestReviewRecordGivesWayInASmallPane covers the record being the first thing dropped, with an
// earlier review to measure from, since the legend is a row longer in that case and it is the case
// the pane is least likely to hold.
func TestReviewRecordGivesWayInASmallPane(t *testing.T) {
	agents := &fakeReviewAgents{}
	m := reviewWithAnEarlierReview(t, newFakeRepo(t.TempDir()), agents)

	require.NotNil(t, m.review.baseRound, "the base key has somewhere to go")
	require.Contains(t, ansi.Strip(m.reviewView()), "agent_id", "the record draws at full height")

	m.helpExpanded = true
	m.height = 20
	view := m.reviewView()

	assert.NotContains(t, ansi.Strip(view), "› name", "and gives way rather than running past the pane")
	assert.LessOrEqual(t, strings.Count(strings.TrimRight(view, "\n"), "\n")+1, m.height)
}

// TestReviewRecordCannotTakeTheKeysWhileItIsDropped covers the band of heights where the pane
// holds a record but this sidebar does not: keys taken by something nobody can see are keys the
// reader has lost.
func TestReviewRecordCannotTakeTheKeysWhileItIsDropped(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)
	m.helpExpanded = true
	m.height = 12

	require.Positive(t, m.detailsLines(), "the pane would hold a record, for this to prove anything")
	require.NotContains(t, ansi.Strip(m.reviewView()), "› name", "and the sidebar drops it anyway")

	next, _ := m.Update(keyMsg("ctrl+t"))
	refused := next.(model)
	assert.False(t, refused.isDetailsFocused(), "so the record cannot take the keys")

	// The diff still has them, which is the whole point of refusing.
	cursor := refused.review.cursor
	next, _ = refused.Update(keyMsg("j"))
	assert.Greater(t, next.(model).review.cursor, cursor)
}

// TestReviewRecordGivesTheKeysBackWhenThePaneShrinks covers a pane resized under a focused
// record stranding the keys the same way.
func TestReviewRecordGivesTheKeysBackWhenThePaneShrinks(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.Update(keyMsg("ctrl+t"))
	focused := next.(model)
	require.True(t, focused.isDetailsFocused())

	focused.helpExpanded = true
	focused.height = 12
	require.NotContains(t, ansi.Strip(focused.reviewView()), "› name")
	assert.False(t, focused.isDetailsFocused(), "the record went, so the keys came back")

	cursor := focused.review.cursor
	next, _ = focused.Update(keyMsg("j"))
	assert.Greater(t, next.(model).review.cursor, cursor)
}

// TestReviewRecordKeysFollowWhatIsDrawn sweeps the heights either side of both thresholds.
func TestReviewRecordKeysFollowWhatIsDrawn(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	taken := 0

	// A standing query widens the header and an expanded legend is the one state where the two
	// legends differ in height, while a pattern being typed would take ctrl+t as a character.
	for _, query := range []string{"", "hello"} {
		for _, expanded := range []bool{false, true} {
			for height := range 60 {
				m.height, m.helpExpanded = height, expanded
				m.review.filter.setQuery(query)

				next, _ := m.Update(keyMsg("ctrl+t"))
				focused := next.(model)
				if !focused.isDetailsFocused() {
					continue
				}

				taken++

				assert.Contains(t, ansi.Strip(focused.reviewView()), "› name",
					"the record took the keys at height %d without being drawn, query=%q expanded=%v",
					height, query, expanded)
			}
		}
	}

	assert.Positive(t, taken, "no height took the keys at all, so the sweep proved nothing")
}

// TestReviewRecordSaysNothingAboutAnAgentThatIsGone covers the diff outliving the row it opened
// from.
func TestReviewRecordSaysNothingAboutAnAgentThatIsGone(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)
	m.listing.agents = nil

	assert.Empty(t, m.reviewDetails().fields)
	assert.Empty(t, m.detailsView())
	assert.NotContains(t, ansi.Strip(m.reviewView()), "agent_id")
}

func fieldValue(t *testing.T, held details, label string) string {
	t.Helper()

	for _, field := range held.fields {
		if field.label == label {
			return field.value
		}
	}

	t.Fatalf("the record carries no %q field", label)

	return ""
}

// TestReviewSidebarKeepsTheWindowWhenThereIsNoRoomForTheDiff covers the narrow window: the
// sidebar is what the screen is for, so the diff gives way rather than both halves being
// squeezed into something neither can be read in.
func TestReviewSidebarKeepsTheWindowWhenThereIsNoRoomForTheDiff(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	split, ok := m.screenColumnSplit()
	require.True(t, ok)
	assert.Equal(t, 30, split.sidebar, "the columns the divider leaves it")
	assert.Equal(t, 170, split.column)
	assert.Equal(t, 30, m.paneWidth(), "which is the column the sidebar is laid out in")

	// A window narrower than the divider was measured against falls back to the configured
	// columns, and the diff keeps the rest.
	m.width = 120

	split, ok = m.screenColumnSplit()
	require.True(t, ok)
	assert.Equal(t, 30, split.sidebar)
	assert.Equal(t, 90, split.column)

	m.width = 45

	_, ok = m.screenColumnSplit()
	assert.False(t, ok, "there is no room for a conversation and a gutter both")
	assert.Equal(t, 45, m.paneWidth(), "so the sidebar takes the window")
	assert.NotContains(t, ansi.Strip(m.View()), "package greet", "and the diff waits")
}

// TestReviewPutsTheSidebarOnItsConfiguredSide covers the split following the layout: the diff
// stands where the panes the window was taken from would have.
func TestReviewPutsTheSidebarOnItsConfiguredSide(t *testing.T) {
	tests := []struct {
		name string
		side string
		want string
	}{
		{
			name: "the default puts the panel on the left",
			side: config.SidebarSideLeft,
			want: "greet.go:1",
		},
		{
			name: "and the diff takes that side when it is asked to",
			side: config.SidebarSideRight,
			want: "Review · auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
			m.frame = layout.New(tmux, config.Layout{SidebarSide: tt.side, SidebarWidth: 24})
			m.frame.Attach("%0")
			m = openReview(t, m)

			// The band stands over the diff, so the top row opens with the sidebar's own first line
			// when the panel is on the left and with the band's title when it is on the right.
			first, _, _ := strings.Cut(reviewFrameText(t, m), "\n")
			assert.True(t, strings.HasPrefix(first, tt.want), "the top row opens with %q: %q", tt.want, first)
		})
	}
}
