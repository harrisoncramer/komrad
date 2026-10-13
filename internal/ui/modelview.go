package ui

import (
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// View is the frame, under the terminal modes the panel runs in.
func (m *model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.ReportFocus = true
	view.MouseMode = tea.MouseModeCellMotion

	return view
}

// insideView is the frame the gutters surround, taking the failures in the order they happen.
func (m *model) insideView() string {
	if m.configErr != nil {
		return errui.RenderConfigError(m.term.width, m.configErr)
	}

	if m.startupErr != nil {
		return errui.RenderStartupError(m.term.width, m.startupErr, m.startupFindings)
	}

	return m.fitFrame(strings.TrimRight(m.screenView(), "\n"))
}

// fitFrame pads the panel out to the height of its pane, so a shorter frame covers the rows the
// one before it left behind.
func (m *model) fitFrame(view string) string {
	if m.term.height <= 0 || m.term.width <= 0 {
		return view
	}

	rows := strings.Count(view, "\n") + 1
	if rows >= m.term.height {
		return view
	}

	// Spaces rather than empty rows.
	blank := strings.Repeat(" ", m.term.width)

	return view + strings.Repeat("\n"+blank, m.term.height-rows)
}

// gutter is the blank cells left of what the panel renders.
const gutter = 1

// countInsetCols is the columns the gutter and the divider beside the stage take from the panel.
func (m *model) countInsetCols() int {
	if m.frame.IsSolo() {
		return gutter
	}

	return m.countSidebarChrome()
}

// countSidebarChrome is the columns the gutter and the divider take from a sidebar beside the stage.
func (m *model) countSidebarChrome() int {
	if m.frame.IsSidebarOnRight() {
		return gutter + 1
	}

	return 2*gutter + 1
}

// countInsetLeft is the columns between the left edge of the pane and what the panel renders.
func (m *model) countInsetLeft() int {
	if !m.frame.IsSolo() && m.frame.IsSidebarOnRight() {
		return gutter + 1
	}

	return gutter
}

// renderInset puts the gutter left of the frame, and the divider on the side facing the stage.
func (m *model) renderInset(view string) string {
	if m.term.width <= 0 {
		return view
	}

	pad := strings.Repeat(" ", gutter)
	edged := !m.frame.IsSolo()
	onRight := m.frame.IsSidebarOnRight()

	lines := strings.Split(view, "\n")
	for i, line := range lines {
		line = ansi.Truncate(line, m.term.width, "")
		line += strings.Repeat(" ", max(0, m.term.width-ansi.StringWidth(line)))

		left, right := pad, ""
		switch {
		case edged && onRight:
			left = renderEdgeRow(isRule(line), true)
		case edged:
			right = renderEdgeRow(isRule(line), false)
		}

		lines[i] = left + line + right
	}

	return strings.Join(lines, "\n")
}

// renderEdgeRow is one row of the divider on the side of the panel facing the stage, joined to a rule
// that meets it.
func renderEdgeRow(rule, facingLeft bool) string {
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

func (m *model) screenView() string {
	if (!m.form.isActive() || m.isPopupMenu()) && !m.isMessagesFocused() {
		return m.active().view(m)
	}

	split, ok := m.columnSplit()
	if !ok && m.isMessagesFocused() {
		return m.renderMessagesWindow(m.term.width, m.term.height).view(m.term.height)
	}
	if !ok {
		return m.heldFormView()
	}

	// Asked for before either half is rendered, so the halves are laid out already knowing their
	// column.
	held := m.measuredAt(split.sidebar)

	if m.isMessagesFocused() {
		return m.joinColumns(split, held.active().view(held), m.docColumnView(split))
	}

	// The list is rendered beside the form as the context the form was opened from, and takes none of
	// the keys while it is there.
	return m.joinColumns(split, held.active().view(held), m.heldFormView())
}

// heldFormView is the form with the submitting notice under it, a row the form gives up rather
// than one taken off the top of the panel.
func (m *model) heldFormView() string {
	view := m.joinedFormView()
	if m.term.markingZones {
		return view
	}

	return keyMark.ReplaceAllString(view, "")
}

func (m *model) joinedFormView() string {
	view := m.form.form.View()
	if m.form.form.State == huh.StateCompleted {
		view = m.form.frozen
	}
	if write, ok := m.heldWrite(); ok {
		width, _ := m.formBox()
		view = strings.TrimRight(view, "\n") + "\n" + write.Notice(m.submittingParams(width))
	} else if m.form.refusal != "" {
		width, _ := m.formBox()
		view = strings.TrimRight(view, "\n") + "\n" + m.renderRefusal(width)
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

// joinLeading renders blocks side by side ahead of the form, a divider between each.
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
			row.WriteString(renderDividerRow(false, false))
		}

		if i < len(split[len(leading)]) {
			row.WriteString(split[len(leading)][i])
		}

		out[i] = row.String()
	}

	return strings.Join(out, "\n")
}

func (m *model) renderRefusal(width int) string {
	icon := paint.Panel.Style(kindTone(messageError)).Render(kindIcon(m.glyphs, messageError))

	return icon + " " + paint.Clip(m.form.refusal, max(width-2, 1))
}
