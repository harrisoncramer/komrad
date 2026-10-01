package ui

import (
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
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

// render renders the active screen inside its gutters.
func (m model) render() string {
	if m.configErr != nil || m.startupErr != nil {
		return m.drawInset(m.insideView())
	}

	if m.picker.frozen != "" {
		return m.fitFrame(m.picker.frozen)
	}

	if m.isAwaitingSize() && m.drawn != nil && m.drawn.text != "" {
		return m.drawn.text
	}

	drawn := m.drawInMode(m.findMode())

	if m.drawn != nil {
		m.drawn.text = drawn
	}

	return drawn
}

// insideView is the frame the gutters surround, taking the failures in the order they happen.
func (m model) insideView() string {
	if m.configErr != nil {
		return errui.RenderConfigError(m.width, m.configErr)
	}

	if m.startupErr != nil {
		return errui.RenderStartupError(m.width, m.startupErr, m.startupFindings)
	}

	return m.fitFrame(strings.TrimRight(m.screenView(), "\n"))
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

// gutter is the blank cells left of what the panel draws.
const gutter = 1

// countInsetCols is the columns the gutter and the divider beside the stage take from the panel.
func (m model) countInsetCols() int {
	if m.frame.IsSolo() {
		return gutter
	}

	return m.countSidebarChrome()
}

// countSidebarChrome is the columns the gutter and the divider take from a sidebar beside the stage.
func (m model) countSidebarChrome() int {
	if m.frame.IsSidebarOnRight() {
		return gutter + 1
	}

	return 2*gutter + 1
}

// countInsetLeft is the columns between the left edge of the pane and what the panel draws.
func (m model) countInsetLeft() int {
	if !m.frame.IsSolo() && m.frame.IsSidebarOnRight() {
		return gutter + 1
	}

	return gutter
}

// drawInset puts the gutter left of the frame, and the divider on the side facing the stage.
func (m model) drawInset(view string) string {
	if m.width <= 0 {
		return view
	}

	pad := strings.Repeat(" ", gutter)
	edged := !m.frame.IsSolo()
	onRight := m.frame.IsSidebarOnRight()

	lines := strings.Split(view, "\n")
	for i, line := range lines {
		line = ansi.Truncate(line, m.width, "")
		line += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(line)))

		left, right := pad, ""
		switch {
		case edged && onRight:
			left = drawEdgeRow(isRule(line), true)
		case edged:
			right = drawEdgeRow(isRule(line), false)
		}

		lines[i] = left + line + right
	}

	return strings.Join(lines, "\n")
}

// drawEdgeRow is one row of the divider on the side of the panel facing the stage, joined to a rule
// that meets it.
func drawEdgeRow(rule, facingLeft bool) string {
	if !rule {
		if facingLeft {
			return dimStyle().Render(dividerGlyph) + strings.Repeat(" ", gutter)
		}

		return strings.Repeat(" ", gutter) + dimStyle().Render(dividerGlyph)
	}

	if facingLeft {
		return dimStyle().Render(glyph.TeeLeft + strings.Repeat(glyph.Rule, gutter))
	}

	return dimStyle().Render(strings.Repeat(glyph.Rule, gutter) + glyph.TeeRight)
}

func (m model) screenView() string {
	if !m.form.isActive() && !m.isMessagesFocused() {
		return m.active().view(m.shared())
	}

	split, ok := m.columnSplit()
	if !ok && m.isMessagesFocused() {
		return m.buildMessagesWindow(m.width, m.height).view(m.height)
	}
	if !ok {
		return m.heldFormView()
	}

	// Asked for before either half is drawn, so the halves are laid out already knowing their
	// column.
	held := m.measuredAt(split.sidebar)

	if m.isMessagesFocused() {
		return m.joinColumns(split, held.active().view(held.shared()), m.docColumnView(split))
	}

	// The list is drawn beside the form as the context the form was opened from, and takes none of
	// the keys while it is there.
	return m.joinColumns(split, held.active().view(held.shared()), m.heldFormView())
}

// heldFormView is the form with the submitting notice under it, a row the form gives up rather
// than one taken off the top of the panel.
func (m model) heldFormView() string {
	view := m.form.form.View()
	if m.form.form.State == huh.StateCompleted {
		view = m.form.frozen
	}
	if write, ok := m.heldWrite(); ok {
		width, _ := m.formBox()
		view = strings.TrimRight(view, "\n") + "\n" + write.Notice(m.submittingParams(width))
	}

	leading, _ := m.formLeading()
	if m.form.trailing == nil {
		return joinLeading(leading, view)
	}

	lines := xslice.Map(strings.Split(strings.TrimRight(view, "\n"), "\n"), func(line string) string {
		return strings.TrimRight(line, " ")
	})
	view = strings.Join(lines, "\n")

	width, height := m.formBox()

	return joinLeading(append(leading, view), m.form.trailing(max(width-lipgloss.Width(view)-dividerCols, 1), height))
}

// joinLeading draws blocks side by side ahead of the form, a divider between each.
func joinLeading(leading []string, form string) string {
	if len(leading) == 0 {
		return form
	}

	blocks := slices.Concat(leading, []string{form})
	split := xslice.Map(blocks, func(block string) []string { return strings.Split(strings.TrimRight(block, "\n"), "\n") })
	rows := slices.Max(xslice.Map(split, func(lines []string) int { return len(lines) }))

	out := make([]string, rows)
	for i := range rows {
		var row strings.Builder

		for b, lines := range split[:len(leading)] {
			line := readLineAt(lines, i, 0)
			row.WriteString(line + strings.Repeat(" ", max(lipgloss.Width(leading[b])-lipgloss.Width(line), 0)))
			row.WriteString(drawDividerRow(false, false))
		}

		if i < len(split[len(leading)]) {
			row.WriteString(split[len(leading)][i])
		}

		out[i] = row.String()
	}

	return strings.Join(out, "\n")
}
