package ui

import (
	"testing"

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

// TestTheComposerFitsTheSidebarItIsDrawnIn covers the field being sized from the column rather
// than from the terminal, which is the whole of what huh would otherwise size itself from.
func TestTheComposerFitsTheSidebarItIsDrawnIn(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)
	m.width, m.height = 120, 40

	next, _ := m.updateReview(keyMsg("c"))
	m, ok := next.(model)
	require.True(t, ok)

	for _, row := range splitReviewRows(ansi.Strip(m.reviewSidebarView())) {
		assert.LessOrEqual(t, lipgloss.Width(row), m.paneWidth(), "no row runs past the sidebar")
	}
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
