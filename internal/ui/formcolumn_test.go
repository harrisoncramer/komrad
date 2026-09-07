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
	m.api = api{agents: emptyLists{}, workflows: emptyLists{}, schedules: emptyLists{}, hooks: &fakeHooks{}}

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
	assert.Equal(t, split.rows-formHelpLines, height, "less the row huh draws its own keys in")
	assert.Less(t, width, m.width, "which is narrower than the window the panel was told about")

	for _, row := range strings.Split(m.form.form.View(), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(row), split.column, "and nothing it draws runs past it")
	}
}

// TestFormStaysInsideTheFrame covers the form being drawn inside the window rather than past the
// bottom of it, which is the whole reason the panel tells it how much room it has: the workflow
// form asks for four moments of hooks under everything else and is taller than most terminals.
func TestFormStaysInsideTheFrame(t *testing.T) {
	for _, height := range []int{16, 24, 32, 60} {
		m := resizedTo(t, formColumnModel(t), 160, height)

		opened, cmd := m.newWorkflowForm()
		m = asModel(t, opened)

		m = delivered(t, m, cmd)

		assert.LessOrEqual(t, countLines(m.View()), height, "height=%d", height)
		assert.Contains(t, ansi.Strip(m.View()), "New workflow name", "height=%d opens at the top of itself", height)
	}
}

// TestFormStaysInsideTheFrameUnderABanner covers a refused submit, which puts a banner where the
// submitting notice was and leaves the form up for the retry: the banner is taller than the notice
// the form was measured against, and it cannot be dismissed while a form holds the keys, so
// without a resize the overflow lasts for as long as the person is fixing what was refused.
func TestFormStaysInsideTheFrameUnderABanner(t *testing.T) {
	for _, height := range []int{24, 32, 40} {
		m := resizedTo(t, formColumnModel(t), 160, height)

		opened, cmd := m.newWorkflowForm()
		m = asModel(t, opened)
		m = delivered(t, m, cmd)

		// The submit the service refuses, which holds the form up and puts the reason over it.
		m.pending, m.pendingForm = 1, true
		refused, cmd := m.applyWrite(writeResult{summary: "Could not create the workflow", err: errNoWorkflow})
		m = asModel(t, refused)
		m = delivered(t, m, cmd)

		require.NotNil(t, m.banner, "the reason is on screen")
		require.True(t, m.form.isActive(), "and the form is still up to fix it in")
		assert.LessOrEqual(t, countLines(m.View()), height, "height=%d", height)
	}
}

// TestFormKeepsTheWholePaneOnANarrowWindow covers a window with no room for both halves, where
// the form is worth more whole than beside a list neither of them fits next to.
func TestFormKeepsTheWholePaneOnANarrowWindow(t *testing.T) {
	m := resizedTo(t, formColumnModel(t), nameWidth+formColumnFloor-1, 40)

	m = openWorkflowForm(t, m)

	_, ok := m.columnSplit()
	assert.False(t, ok, "the window cannot hold the list beside the form")
	assert.Equal(t, m.width, m.paneWidth(), "so the form is drawn in the whole of the pane")

	width, _ := m.formBox()
	assert.Equal(t, m.width, width)
}
