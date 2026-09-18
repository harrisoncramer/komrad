package ui

import (
	"strings"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/paint"

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
	m.api = api{
		agents:    emptyLists{},
		workflows: emptyLists{},
		schedules: emptyLists{},
		hooks:     &fakeHooks{},
	}

	return m
}

func openWorkflowForm(t *testing.T, m model) model {
	t.Helper()

	opened, _ := m.newWorkflowForm()

	return asModel(t, opened)
}

// TestFormTakesTheWindowBesideTheList covers a form that asks for something being drawn in a
// column of its own, with the list it was opened over still beside it.
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

// TestFormHandsTheWindowBackOnClose covers the way off a form giving up the zoom it was
// holding.
func TestFormHandsTheWindowBackOnClose(t *testing.T) {
	m := formColumnModel(t)
	m = openWorkflowForm(t, m)
	require.True(t, m.frame.IsSolo())

	closed, _ := m.closeForm()
	m = asModel(t, closed)

	assert.False(t, m.frame.IsSolo(), "closing the form gives the other panes their share back")
	assert.Equal(t, m.width, m.paneWidth(), "and the list goes back to the whole of the pane")
}

// TestConfirmDrawsBesideTheList covers a form that only wants an answer being drawn in the
// column too.
func TestConfirmDrawsBesideTheList(t *testing.T) {
	m := formColumnModel(t)

	opened, _ := m.archiveWorkflowConfirm(workflowstore.Workflow{ID: "w1", Name: "backend"})
	m = asModel(t, opened)

	require.True(t, m.form.isActive())
	assert.True(t, m.frame.IsSolo(), "a confirmation takes the window for as long as it is up")

	split, ok := m.columnSplit()
	require.True(t, ok, "and draws its column")
	assert.Equal(t, split.sidebar, m.paneWidth(), "with the list laid out against its own column")

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "backend", "the prompt is on screen")
	assert.Contains(t, view, "auth", "and so is the list it was opened over")
}

// TestConfirmKeepsTheWholePaneOnANarrowWindow covers a window with no room for both halves, where
// the confirmation is drawn where it always was.
func TestConfirmKeepsTheWholePaneOnANarrowWindow(t *testing.T) {
	m := resizedTo(t, formColumnModel(t), nameWidth+columnFloor-1, 40)

	opened, _ := m.archiveWorkflowConfirm(workflowstore.Workflow{ID: "w1", Name: "backend"})
	m = asModel(t, opened)

	_, ok := m.columnSplit()
	assert.False(t, ok, "the window cannot hold the list beside the prompt")
	assert.Equal(t, m.width, m.paneWidth(), "so the prompt is drawn in the whole of the pane")
	assert.Contains(t, ansi.Strip(m.View()), "backend")
}

// TestFormIsSizedToItsColumn covers the form being laid out in the column it is drawn in rather
// than in the pane the panel had a moment before.
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

		assert.LessOrEqual(t, paint.CountLines(m.View()), height, "height=%d", height)
		assert.Contains(t, ansi.Strip(m.View()), "New workflow name", "height=%d opens at the top of itself", height)
	}
}

// TestFormStaysInsideTheFrameUnderARefusal covers a refused submit, which reports the reason into
// the region under the list and leaves the form up for the retry.
func TestFormStaysInsideTheFrameUnderARefusal(t *testing.T) {
	for _, height := range []int{24, 32, 40} {
		m := resizedTo(t, formColumnModel(t), 160, height)

		opened, cmd := m.newWorkflowForm()
		m = asModel(t, opened)
		m = delivered(t, m, cmd)

		// The submit the service refuses, which holds the form up and puts the reason over it.
		m.pending, m.held = 1, heldWrite{active: true}
		refused, cmd := m.applyWrite(writeResult{summary: "Could not create the workflow", err: errNoWorkflow})
		m = asModel(t, refused)
		m = delivered(t, m, cmd)

		require.NotEmpty(t, m.messages.items, "the reason is on screen")
		require.True(t, m.form.isActive(), "and the form is still up to fix it in")
		assert.LessOrEqual(t, paint.CountLines(m.View()), height, "height=%d", height)
	}
}

// TestAHeldFormCarriesItsOwnNotice covers where the submitting notice is drawn: under the form
// rather than above the panel.
func TestAHeldFormCarriesItsOwnNotice(t *testing.T) {
	for _, height := range []int{24, 32, 40} {
		m := resizedTo(t, formColumnModel(t), 160, height)

		opened, cmd := m.newWorkflowForm()
		m = asModel(t, opened)
		m = delivered(t, m, cmd)

		m.pending, m.held = 1, heldWrite{active: true, since: time.Now()}
		m = delivered(t, m, formResized())

		lines := strings.Split(strings.TrimRight(ansi.Strip(m.View()), "\n"), "\n")

		assert.NotContains(t, lines[0], "submitting", "height=%d the panel keeps its top row", height)
		assert.Contains(t, lastFilled(lines), "submitting…", "height=%d the notice sits under the form", height)
		assert.LessOrEqual(t, paint.CountLines(m.View()), height, "height=%d", height)
	}
}

// lastFilled is the bottom row of a view that says anything.
func lastFilled(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}

	return ""
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

// TestFormStillsTheCursorOnTheListBesideIt covers the list a form is opened over, whose row
// cursor stops blinking once the form has the keys.
func TestFormStillsTheCursorOnTheListBesideIt(t *testing.T) {
	m := formColumnModel(t)
	m.rowCursor = newRowCursor()
	m.rowCursor.Blink = true
	require.True(t, m.isListFocused(), "the list holds the keys to start with")
	require.Equal(t, blankCursor, m.rowCursorPrefix(), "so its cursor is mid-blink")

	m = openWorkflowForm(t, m)

	assert.False(t, m.isListFocused(), "the form holds them once it is up")
	assert.Equal(t, paint.Panel.Cursor().Render(rowCursorGlyph), m.rowCursorPrefix(), "and its cursor stands still")
}

// TestFormRefusalIsReadableFromInsideTheForm covers reaching the whole reason without leaving
// the form it was refused from.
func TestFormRefusalIsReadableFromInsideTheForm(t *testing.T) {
	m := resizedTo(t, formColumnModel(t), 160, 40)

	opened, cmd := m.newWorkflowForm()
	m = asModel(t, opened)
	m = delivered(t, m, cmd)

	m.pending, m.held = 1, heldWrite{active: true}
	refused, cmd := m.applyWrite(writeResult{summary: "Could not create the workflow", err: errNoWorkflow})
	m = asModel(t, refused)
	m = delivered(t, m, cmd)

	require.True(t, m.form.isActive())

	reading := send(t, m, messagesFocusKey)

	require.True(t, reading.isMessagesFocused(), "the focus key reaches the region past the form")

	view := ansi.Strip(reading.View())
	assert.Contains(t, view, "Could not create the workflow")
	assert.Contains(t, view, errNoWorkflow.Error(), "the whole reason is drawn in the block")
	assert.NotContains(t, view, "New workflow name", "the form gives up what is drawn in its column")

	back := send(t, reading, messagesFocusKey)

	assert.False(t, back.isMessagesFocused())
	assert.Contains(t, ansi.Strip(back.View()), "New workflow name", "and takes it back")
	assert.True(t, back.form.isActive(), "the form was never closed")
}

// TestRenameDrawsBesideTheList covers the rename form being drawn in a column of its own like
// every other form.
func TestRenameDrawsBesideTheList(t *testing.T) {
	m := formColumnModel(t)

	opened, _ := m.openRename(agentstore.Agent{ID: "a1", Name: "api-refactor"})
	m = asModel(t, opened)

	require.True(t, m.form.isActive())
	assert.True(t, m.frame.IsSolo(), "the form takes the window while it is up")

	split, ok := m.columnSplit()
	require.True(t, ok, "and draws its column")
	assert.Equal(t, split.sidebar, m.paneWidth(), "with the list laid out against its own column")

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "api-refactor", "the prompt is on screen")
	assert.Contains(t, view, "auth", "and so is the list it was opened over")
}
