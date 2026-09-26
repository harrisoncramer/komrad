package ui

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"komrad/internal/errui"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// View is the frame, under the terminal modes the panel runs in.
func (m model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.ReportFocus = true
	view.MouseMode = tea.MouseModeCellMotion

	return view
}

// render renders the active screen, taking the failures in the order they happen.
func (m model) render() string {
	if m.configErr != nil {
		return errui.RenderConfigError(m.width, m.configErr)
	}

	if m.startupErr != nil {
		return errui.RenderStartupError(m.width, m.startupErr, m.startupFindings)
	}

	//nolint:sloglint // temporary trace, no context here to carry a logger on
	slog.Default().Info("panel trace: drawing", "width", m.width, "height", m.height, "solo", m.frame.IsSolo(), "awaiting", m.awaitingSize, "screen", int(m.screen))

	if m.awaitingSize && m.drawn != nil && m.drawn.text != "" {
		return m.drawn.text
	}

	drawn := m.drawEdge(m.fitFrame(strings.TrimRight(m.screenView(), "\n")))

	if m.drawn != nil {
		m.drawn.text = drawn
	}

	return drawn
}

// submittingNotice is the line drawn under a held form, counting up once the write has taken
// long enough that a still line would read as a wedged panel.
func submittingNotice(since, now time.Time) string {
	const patience = 2 * time.Second

	elapsed := now.Sub(since)
	if since.IsZero() || elapsed < patience {
		return "  submitting…"
	}

	if elapsed < time.Minute {
		return fmt.Sprintf("  submitting… %ds · esc to keep working", int(elapsed.Seconds()))
	}

	return fmt.Sprintf("  submitting… %dm%02ds · esc to keep working", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
}

// fitFrame pads the panel out to the height of its pane, so a shorter frame covers the rows the
// one before it left behind.
func (m model) fitFrame(view string) string {
	if m.height <= 0 || m.width <= 0 {
		return view
	}

	rows := strings.Count(view, "\n") + 1
	if rows >= m.height {
		return view
	}

	// Spaces rather than empty rows.
	blank := strings.Repeat(" ", m.width)

	return view + strings.Repeat("\n"+blank, m.height-rows)
}

// countEdgeCols is the columns the divider beside the stage takes from the panel.
func (m model) countEdgeCols() int {
	if m.frame.IsSolo() {
		return 0
	}

	return dividerCols
}

// drawEdge draws the divider down the side of the panel that faces the stage.
func (m model) drawEdge(view string) string {
	if m.countEdgeCols() == 0 || m.width <= 0 {
		return view
	}

	edge := dimStyle.Render(dividerGlyph)
	lines := strings.Split(view, "\n")

	for i, line := range lines {
		line = ansi.Truncate(line, m.width, "")
		line += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(line)))

		if m.frame.IsSidebarOnRight() {
			lines[i] = edge + line
		} else {
			lines[i] = line + edge
		}
	}

	return strings.Join(lines, "\n")
}

func (m model) screenView() string {
	if !m.form.isActive() {
		return m.active().view(m.shared())
	}

	form := m.heldFormView()

	split, ok := m.columnSplit()
	if !ok {
		return form
	}

	// Asked for before either half is drawn, so the halves are laid out already knowing their
	// column.
	held := m.measuredAt(split.sidebar)

	// The list is drawn beside the form as the context the form was opened from, and takes none of
	// the keys while it is there.
	block := form
	if m.isMessagesFocused() {
		// A reason is longer than the rows the region keeps, so reading it borrows the column the
		// form is drawn in.
		block = m.docColumnView(split)
	}

	return m.joinColumns(split, held.active().view(held.shared()), block)
}

// heldFormView is the form with the submitting notice under it, a row the form gives up rather
// than one taken off the top of the panel.
func (m model) heldFormView() string {
	view := m.form.form.View()
	if !m.held.active {
		return view
	}

	return strings.TrimRight(view, "\n") + "\n" + dimStyle.Render(submittingNotice(m.held.since, time.Now()))
}
