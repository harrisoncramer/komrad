package ui

import (
	"fmt"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/view"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// searchFiller pads a line out past any column the preview is drawn in, long enough that a
// window of another width wraps the transcript to a different number of rows.
const searchFiller = " and then a good deal more of it said in the same breath, going on for long " +
	"enough that no column the panel draws holds the whole of it on one row"

// searchTranscript is a conversation long enough that the preview cannot show all of it, with the
// word the hit matched on well past the first screenful.
func searchTranscript() (content string, offset, length int64) {
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "line %d of the conversation before the match%s\n\n", i, searchFiller)
	}

	offset = int64(b.Len())
	turn := "the reconcile loop is what settles this\n"
	b.WriteString(turn)

	for i := range 200 {
		fmt.Fprintf(&b, "\nline %d of the conversation after the match\n", i)
	}

	return b.String(), offset, int64(len(turn))
}

// searchPreviewModel is the search screen holding the window with one hit under the cursor, which
// is the state the preview is drawn in.
func searchPreviewModel(t *testing.T) (model, *recordingTmux) {
	t.Helper()

	tmux := &recordingTmux{width: 200, height: 30}
	m := infoModel(tmux)
	// The search holds the whole window, so this is the size the rest of the panel's screens are
	// measured at.
	m.width, m.height = 200, 30

	next, _ := m.enterSearch()
	m, ok := next.(model)
	require.True(t, ok)

	content, offset, length := searchTranscript()
	m.search.typing = false
	m.search.query = "reconcile"
	m.search.turns = []searchHit{{
		agentID: "a1",
		name:    "billing",
		offset:  offset,
		length:  length,
		snippet: marked("reconcile"),
	}}

	m.applyPreview(previewResult{
		agentID: "a1",
		query:   "reconcile",
		content: content,
	})

	return m, tmux
}

// TestSearchTakesTheWindow covers the screen taking the window off the panes it was sharing with
// and handing it back on the way out.
func TestSearchTakesTheWindow(t *testing.T) {
	m, _ := searchPreviewModel(t)
	assert.True(t, m.frame.IsSolo(), "the search draws both halves itself")

	m.search.typing = true
	next, _ := m.route(keyMsg("esc"))
	left, ok := next.(model)
	require.True(t, ok)

	assert.Equal(t, screenAgents, left.screen)
	assert.False(t, left.frame.IsSolo(), "the panes have their share back")
}

// TestSearchPreviewOpensOnTheMatch covers the window opening where the hit was found rather than at
// the top of the transcript.
func TestSearchPreviewOpensOnTheMatch(t *testing.T) {
	m, _ := searchPreviewModel(t)

	require.NotEmpty(t, m.search.preview.doc.rows)
	assert.Positive(t, m.search.preview.doc.top, "the preview is scrolled to the match")

	shown := ansi.Strip(m.searchPreviewView(previewSplit(t, m)))
	assert.Contains(t, shown, "the reconcile loop", "the matched turn is on screen")
	assert.NotContains(t, shown, "line 0 of the conversation before", "the top of the transcript is not")
	assert.Contains(t, shown, "line 199 of the conversation before",
		"the rows leading up to the match are")
}

// TestSearchPreviewScrolls covers the scroll keys, which are what the panel took on when the
// pager that was doing the scrolling went away.
func TestSearchPreviewScrolls(t *testing.T) {
	m, _ := searchPreviewModel(t)
	opened := m.search.preview.doc.top

	m = pressSearch(t, m, "ctrl+n")
	assert.Equal(t, opened+docScrollStep, m.search.preview.doc.top, "a press moves a few lines")

	m = pressSearch(t, m, "ctrl+p")
	assert.Equal(t, opened, m.search.preview.doc.top, "and back again")

	for range 100 {
		m = pressSearch(t, m, "ctrl+p")
	}
	assert.Zero(t, m.search.preview.doc.top, "the top of the transcript is as far up as it goes")

	for range 200 {
		m = pressSearch(t, m, "ctrl+n")
	}
	split := previewSplit(t, m)
	assert.Equal(t, len(m.search.preview.doc.rows)-split.rows, m.search.preview.doc.top,
		"the last screenful is as far down as it goes")
	assert.Contains(t, ansi.Strip(m.searchPreviewView(split)), "line 199 of the conversation after",
		"the end of the transcript is drawn rather than empty rows past it")
}

// TestSearchPreviewFollowsTheCursor covers stepping to another hit, which fetches its own
// transcript and has to open on its own match rather than where the last one was left.
func TestSearchPreviewFollowsTheCursor(t *testing.T) {
	m, _ := searchPreviewModel(t)

	m = pressSearch(t, m, "ctrl+n")
	moved := m.search.preview.doc.top

	content, offset, length := searchTranscript()
	m.search.turns = append(m.search.turns, searchHit{
		agentID: "a2",
		name:    "payouts",
		offset:  offset,
		length:  length,
		snippet: marked("reconcile"),
	})
	m.search.cursor = 2
	m.applyPreview(previewResult{
		agentID: "a2",
		query:   "reconcile",
		content: content,
	})

	assert.NotEqual(t, moved, m.search.preview.doc.top, "the preview is put back on the new match")
	assert.Contains(t, ansi.Strip(m.searchPreviewView(previewSplit(t, m))), "the reconcile loop")
}

// TestSearchDrawsBothHalves covers the two columns being joined into one frame.
func TestSearchDrawsBothHalves(t *testing.T) {
	m, _ := searchPreviewModel(t)

	split := previewSplit(t, m)
	rows := drawnLines(m.searchView())

	assert.LessOrEqual(t, len(rows), m.height, "the frame is no taller than the window")
	for _, row := range rows {
		assert.LessOrEqual(t, lipgloss.Width(row), m.width, "no row runs past the window")
	}

	joined := strings.Join(rows, "\n")
	assert.Contains(t, joined, "billing", "the hits are drawn")
	assert.Contains(t, joined, "the reconcile loop", "the transcript is drawn beside them")

	// The sidebar is on the left by default, so the transcript starts at its share and a row
	// carrying both has to be wider than the sidebar alone.
	assert.Greater(t, lipgloss.Width(rows[0]), split.sidebar, "the halves stand side by side")
}

// TestSearchPutsTheHitsWhereTheLayoutAsks covers the panel's own column standing where the panes
// it took the window from would have.
func TestSearchPutsTheHitsWhereTheLayoutAsks(t *testing.T) {
	tests := []struct {
		name string
		side string
		want string
	}{
		{
			name: "the default puts the hits on the left",
			side: config.SidebarSideLeft,
			want: view.Spell(view.GlyphSearch) + "  Search",
		},
		{
			name: "and the transcript takes that side when it is asked to",
			side: config.SidebarSideRight,
			want: "line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, tmux := searchPreviewModel(t)
			m.frame = layout.New(tmux, config.Layout{SidebarSide: tt.side, SidebarWidth: 40})
			m.frame.Attach("%0")
			m.frame.Solo(true)
			m.drawPreview()

			first, _, _ := strings.Cut(ansi.Strip(m.searchView()), "\n")
			assert.True(t, strings.HasPrefix(strings.TrimLeft(first, " "), tt.want),
				"the top row opens with %q: %q", tt.want, first)
		})
	}
}

// TestSearchWithoutTheRoomDrawsTheHitsAlone covers a window too narrow to carry both halves,
// where the hits take the whole of it and the transcript waits for the room.
func TestSearchWithoutTheRoomDrawsTheHitsAlone(t *testing.T) {
	m, _ := searchPreviewModel(t)
	m.width = nameWidth + columnFloor - 1
	m.drawPreview()

	_, ok := m.screenColumnSplit()
	assert.False(t, ok, "there is no room for a split")

	view := ansi.Strip(m.searchView())
	assert.Contains(t, view, "billing", "the hits are still drawn")
	assert.NotContains(t, view, "the reconcile loop", "the transcript is not")
}

// TestSearchPreviewWithoutATranscript covers a hit on an agent whose conversation has not been
// written yet.
func TestSearchPreviewWithoutATranscript(t *testing.T) {
	m, _ := searchPreviewModel(t)

	m.applyPreview(previewResult{agentID: "a1", query: "reconcile"})

	assert.Equal(t, noTranscriptMarkdown, m.search.preview.conversation)

	rows := drawnLines(m.searchView())
	assert.Contains(t, strings.Join(rows, "\n"), "no transcript yet")
	for _, row := range rows {
		assert.LessOrEqual(t, lipgloss.Width(row), m.width,
			"a column shorter than the window does not let the other half run past it")
	}
}

// TestSearchPreviewRewrapsOnResize covers a window that changed width rewrapping the transcript.
func TestSearchPreviewRewrapsOnResize(t *testing.T) {
	m, _ := searchPreviewModel(t)
	before := len(m.search.preview.doc.rows)

	next, _ := m.route(tea.WindowSizeMsg{Width: 120, Height: 30})
	resized, ok := next.(model)
	require.True(t, ok)

	assert.NotEqual(t, before, len(resized.search.preview.doc.rows), "the transcript is wrapped again")
	for _, row := range strings.Split(resized.searchView(), "\n") {
		assert.Equal(t, resized.width, lipgloss.Width(row),
			"and every row of the frame is squared off, so neither half slides under the other")
	}
}

// TestSearchStandsDownTheStageKeys covers the keys that fill a pane beside the panel, which have
// no pane anybody can see while the search is holding the window.
func TestSearchStandsDownTheStageKeys(t *testing.T) {
	m, tmux := searchPreviewModel(t)
	respawns := len(tmux.respawns)

	for _, k := range []string{"l", "L", "T"} {
		next, _ := m.route(keyMsg(k))
		_, ok := next.(model)
		require.True(t, ok)
	}

	assert.Len(t, tmux.respawns, respawns, "nothing is drawn into a pane nobody can see")
}

// TestSearchPreviewKeepsItsPlaceOnAHeightChange covers a window that grew or shrank without
// changing width, which rewraps nothing and so has no reason to take the reader back to the match.
func TestSearchPreviewKeepsItsPlaceOnAHeightChange(t *testing.T) {
	m, _ := searchPreviewModel(t)

	m = pressSearch(t, m, "ctrl+n")
	m = pressSearch(t, m, "ctrl+n")
	scrolled := m.search.preview.doc.top
	require.NotEqual(t, 0, scrolled)

	next, _ := m.route(tea.WindowSizeMsg{Width: m.width, Height: 44})
	taller, ok := next.(model)
	require.True(t, ok)

	assert.Equal(t, scrolled, taller.search.preview.doc.top, "the reader keeps their place")
}

// TestSearchPreviewDrawsOnceThereIsRoom covers a window that had no rows to give the preview and
// then grew, which changes no width and would otherwise leave the column empty until the cursor moved.
func TestSearchPreviewDrawsOnceThereIsRoom(t *testing.T) {
	m, _ := searchPreviewModel(t)

	m.height = 0
	m.drawPreview()
	require.Empty(t, m.search.preview.doc.rows, "an unmeasured window has no rows to draw into")

	next, _ := m.route(tea.WindowSizeMsg{Width: m.width, Height: 30})
	grown, ok := next.(model)
	require.True(t, ok)

	assert.NotEmpty(t, grown.search.preview.doc.rows, "the rows it was owed are drawn")
}

// TestSearchHandsTheWindowBackToALandingWrite covers a create that comes back while the search is
// up: the write puts the new agent's own pane on screen.
func TestSearchHandsTheWindowBackToALandingWrite(t *testing.T) {
	m, tmux := searchPreviewModel(t)
	require.True(t, m.frame.IsSolo())

	m.pending = 1
	next, _ := m.applyWrite(writeResult{
		summary: "Could not create the agent",
		written: &written{row: agentRowKey("a9"), pane: "%9"},
	})
	landed, ok := next.(model)
	require.True(t, ok)

	assert.Equal(t, screenAgents, landed.screen, "the write takes the panel to the row it wrote")
	assert.False(t, landed.frame.IsSolo(), "and the search lets go of the window on the way")
	assert.Equal(t, 1, tmux.swaps, "so the new agent's own pane is the one on screen")
}

func previewSplit(t *testing.T, m model) columnSplit {
	t.Helper()

	split, ok := m.screenColumnSplit()
	require.True(t, ok, "the window holds both halves")

	return split
}

func pressSearch(t *testing.T, m model, k string) model {
	t.Helper()

	next, _ := m.route(keyMsg(k))
	got, ok := next.(model)
	require.True(t, ok)

	return got
}

// TestSearchPreviewMovesToTheHoveredTurn covers stepping between two turns of one conversation,
// which moves the transcript already held rather than fetching it again.
func TestSearchPreviewMovesToTheHoveredTurn(t *testing.T) {
	m, _ := searchPreviewModel(t)

	content, _, length := searchTranscript()
	early := strings.Index(content, "line 3 of the conversation before")
	m.search.turns = append(m.search.turns, searchHit{
		agentID: "a1",
		name:    "billing",
		seq:     0,
		offset:  int64(early),
		length:  length,
		snippet: marked("conversation"),
	})
	m.search.turns[0].seq = 7
	m.search.cursor = 1
	m.applyPreview(previewResult{
		agentID: "a1",
		query:   "reconcile",
		content: content,
	})
	onLater := m.search.preview.doc.top
	require.Positive(t, onLater)

	m = pressSearch(t, m, "j")
	assert.Equal(t, 2, m.search.cursor)
	assert.Less(t, m.search.preview.doc.top, onLater, "the preview moves up to the earlier turn")
	assert.Equal(t, "a1", m.search.preview.agentID, "without dropping the transcript it held")

	m = pressSearch(t, m, "k")
	assert.Equal(t, onLater, m.search.preview.doc.top, "and back")
}

// TestSearchPagesWhenTheCursorReachesTheEnd covers the page after the hits held being asked for
// once the cursor stands on the last row, and its hits following the ones already drawn.
func TestSearchPagesWhenTheCursorReachesTheEnd(t *testing.T) {
	m, _ := searchPreviewModel(t)
	token := "page-2"
	m.search.next = &token

	m = pressSearch(t, m, "G")
	require.Equal(t, 1, m.search.cursor, "the last row is the one turn")
	assert.True(t, m.search.paging, "the next page is on its way")

	m.applySearch(searchResult{
		query: "stale",
		hits:  hits("z"),
		paged: true,
	})
	assert.Len(t, m.search.turns, 1, "a page for a query the input has moved past is dropped")

	m.applySearch(searchResult{
		query: "reconcile",
		hits:  hits("a2"),
		paged: true,
	})
	assert.Len(t, m.search.turns, 2, "the page's hits follow the ones held")
	assert.Equal(t, 1, m.search.cursor, "the cursor stays where it was")
	assert.False(t, m.search.paging)
	assert.Nil(t, m.search.next, "and there is no page after it")
	assert.Len(t, m.searchRows(), 4)

	m = pressSearch(t, m, "G")
	assert.False(t, m.search.paging, "the end of the last page asks for nothing more")
}
