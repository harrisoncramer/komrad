package ui

import (
	"strings"
	"testing"

	workflowstore "weave/internal/store/workflow"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// formColumnModel is the agents list on a window wide enough to hold a form beside it.
func formColumnModel(t *testing.T) model {
	t.Helper()

	tmux := &recordingTmux{width: 200, height: 40}
	m := infoModel(tmux)
	m.width, m.height = 200, 40
	m.api = api{hooks: &fakeHooks{}}

	return m
}

func openWorkflowForm(t *testing.T, m model) model {
	t.Helper()

	opened, _ := m.newWorkflowForm()

	return asModel(t, opened)
}

// TestFormTakesTheWindowBesideTheList covers a form that asks for something being drawn in a
// column of its own, with the list it was opened over still beside it, since the list is the
// context the form was opened from.
func TestFormTakesTheWindowBesideTheList(t *testing.T) {
	m := formColumnModel(t)
	require.False(t, m.frame.IsSolo(), "the agents list shares the window")

	m = openWorkflowForm(t, m)

	assert.True(t, m.frame.IsSolo(), "a form with a column takes the window")

	split, ok := m.columnSplit()
	require.True(t, ok, "and the window holds both halves")
	assert.Equal(t, split.sidebar, m.paneWidth(), "the list is laid out against its own column")

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "New workflow name", "the form is on screen")
	assert.Contains(t, view, "auth", "and so is the row it was opened from")

	for _, row := range strings.Split(m.View(), "\n") {
		assert.Equal(t, m.width, lipgloss.Width(row),
			"every row of the frame is squared off, so neither half slides under the other")
	}
}

// TestFormHandsTheWindowBackOnClose covers the way off a form, since the panes beside the panel
// are covered by the zoom the form was holding for as long as it holds it.
func TestFormHandsTheWindowBackOnClose(t *testing.T) {
	m := formColumnModel(t)
	m = openWorkflowForm(t, m)
	require.True(t, m.frame.IsSolo())

	closed, _ := m.closeForm()
	m = asModel(t, closed)

	assert.False(t, m.frame.IsSolo(), "closing the form gives the other panes their share back")
	assert.Equal(t, m.width, m.paneWidth(), "and the list goes back to the whole of the pane")
}

// TestConfirmDrawsWithoutAColumn covers a form that only wants an answer, which has nothing to
// fill a column with and so leaves the window as it found it.
func TestConfirmDrawsWithoutAColumn(t *testing.T) {
	m := formColumnModel(t)

	opened, _ := m.archiveWorkflowConfirm(workflowstore.Workflow{ID: "w1", Name: "backend"})
	m = asModel(t, opened)

	require.True(t, m.form.isActive())
	assert.False(t, m.frame.IsSolo(), "a confirmation takes no window")

	_, ok := m.columnSplit()
	assert.False(t, ok, "and draws no column")
	assert.Contains(t, ansi.Strip(m.View()), "backend", "the prompt is on screen")
}

// TestFormIsSizedToItsColumn covers the form being laid out in the column it is drawn in rather
// than in the pane the panel had a moment before, since huh takes its width from a window size
// and the window the panel was sent is the whole terminal.
func TestFormIsSizedToItsColumn(t *testing.T) {
	m := formColumnModel(t)
	m = openWorkflowForm(t, m)

	split, ok := m.columnSplit()
	require.True(t, ok)

	width, height := m.formBox()
	assert.Equal(t, split.column, width, "the form has its own column and not the terminal")
	assert.Equal(t, split.rows, height)
	assert.Less(t, width, m.width, "which is narrower than the window the panel was told about")

	for _, row := range strings.Split(m.form.form.View(), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(row), split.column, "and nothing it draws runs past it")
	}
}

// TestFormKeepsTheWholePaneOnANarrowWindow covers a window with no room for both halves, where
// the form is worth more whole than beside a list neither of them fits next to.
func TestFormKeepsTheWholePaneOnANarrowWindow(t *testing.T) {
	m := formColumnModel(t)
	m.width = nameWidth + formColumnFloor - 1

	m = openWorkflowForm(t, m)

	_, ok := m.columnSplit()
	assert.False(t, ok, "the window cannot hold the list beside the form")
	assert.Equal(t, m.width, m.paneWidth(), "so the form is drawn in the whole of the pane")

	width, _ := m.formBox()
	assert.Equal(t, m.width, width)
}
