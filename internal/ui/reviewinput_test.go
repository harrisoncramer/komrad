package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheComposerIsDrawnAsAnOrdinaryField covers the composer being the panel's own text field
// rather than a widget of its own: the title, the keys under it, and the bar every other field
// is drawn with.
func TestTheComposerIsDrawnAsAnOrdinaryField(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.updateReview(keyMsg("c"))
	m, ok := next.(model)
	require.True(t, ok)
	require.True(t, m.review.composing)

	shown := ansi.Strip(m.reviewSidebarView())

	assert.Contains(t, shown, "Comment")
	assert.Contains(t, shown, "opens $EDITOR")
	assert.Contains(t, shown, "┃", "the field carries the bar huh draws every other one with")
}

// TestTheComposerKeepsReturnForItself covers the one key the panel's other fields spend on
// moving to the next one: a comment is prose, so return has to make a line in it.
func TestTheComposerKeepsReturnForItself(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.updateReview(keyMsg("c"))
	m, ok := next.(model)
	require.True(t, ok)

	m = typeReviewMessage(t, m, "one")

	next, _ = m.updateReview(keyMsg("enter"))
	m, ok = next.(model)
	require.True(t, ok)

	require.True(t, m.review.composing, "the return did not send it")
	assert.Equal(t, "one\n", m.review.editor.value(), "it opened a second line")
}

// TestTheComposerTakesAWidthItWasNotBuiltAt covers the field being sized from a resize rather
// than at build time, which is what makes it follow a divider being dragged.
func TestTheComposerTakesAWidthItWasNotBuiltAt(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = m.resizeTo(tea.WindowSizeMsg{Width: 24, Height: m.height})

	next, _ := m.updateReview(keyMsg("c"))
	m, ok := next.(model)
	require.True(t, ok)

	built := widestFieldRow(t, m)

	m = m.resizeTo(tea.WindowSizeMsg{Width: 200, Height: m.height})

	assert.Greater(t, widestFieldRow(t, m), built, "the field took the wider sidebar")
	assert.LessOrEqual(t, widestFieldRow(t, m), m.paneWidth(), "and stayed inside it")
}

// TestTheComposerKeepsEveryLineAtANarrowSidebar covers the height huh is given: the title and
// the hint wrap as the sidebar narrows, and a height counted rather than measured cuts the
// bottom off what has been typed.
func TestTheComposerKeepsEveryLineAtANarrowSidebar(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = m.resizeTo(tea.WindowSizeMsg{Width: 22, Height: m.height})

	next, _ := m.updateReview(keyMsg("ctrl+s"))
	m, ok := next.(model)
	require.True(t, ok)
	require.True(t, m.review.releasing)

	for i, line := range []string{"one", "two", "three", "four"} {
		if i > 0 {
			next, _ = m.updateReview(keyMsg("enter"))
			m, ok = next.(model)
			require.True(t, ok)
		}

		m = typeReviewLine(t, m, line)
	}

	shown := ansi.Strip(m.reviewSidebarView())
	for _, word := range []string{"one", "two", "three", "four"} {
		assert.Contains(t, shown, word, "every line typed into the field is drawn")
	}

	for _, row := range splitReviewRows(shown) {
		assert.LessOrEqual(t, lipgloss.Width(row), m.paneWidth(), "no row runs past the sidebar")
	}
}

// typeReviewLine types one line into the field, saying nothing about what stands above it.
func typeReviewLine(t *testing.T, m model, words string) model {
	t.Helper()

	for _, letter := range words {
		next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{letter}})
		written, ok := next.(model)
		require.True(t, ok)
		m = written
	}

	return m
}

// widestFieldRow is the longest row of the field itself, which is every row drawn with the bar
// huh puts down its left.
func widestFieldRow(t *testing.T, m model) int {
	t.Helper()

	widest := 0
	for _, row := range splitReviewRows(ansi.Strip(m.reviewSidebarView())) {
		if !strings.HasPrefix(row, "┃") {
			continue
		}

		widest = max(widest, lipgloss.Width(row))
	}

	require.Positive(t, widest, "the field is on screen")

	return widest
}

// splitReviewRows is a rendered block as its rows.
func splitReviewRows(rendered string) []string {
	rows := []string{}
	row := ""

	for _, letter := range rendered {
		if letter == '\n' {
			rows = append(rows, row)
			row = ""

			continue
		}

		row += string(letter)
	}

	return append(rows, row)
}
