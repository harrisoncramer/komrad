package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type detailsState struct {
	focused bool
	cursor  int
	row     string
	flashed int
}

type details struct {
	id      string
	kind    string
	title   string
	notices []notice
	fields  []recordField
}

func (m model) hoveredDetails() details {
	spec := m.screen.spec().details
	if spec == nil {
		return details{}
	}

	return spec.hovered(m)
}

func (m model) isDetailsScreen() bool {
	return m.screen.spec().details != nil
}

func (m model) detailsCursor(held details) int {
	if m.details.row != held.id {
		return 0
	}

	return min(m.details.cursor, max(len(held.fields)-1, 0))
}

func (m model) isDetailsDrawn(held details) bool {
	if len(held.fields) == 0 || m.detailsLines() == 0 {
		return false
	}

	spec := m.screen.spec().details
	if spec == nil || spec.fits == nil {
		return true
	}

	return spec.fits(m)
}

func (m model) isDetailsFocused() bool {
	return m.details.focused && m.isDetailsDrawn(m.hoveredDetails())
}

func (m model) toggleDetailsFocus() (tea.Model, tea.Cmd) {
	if m.isDetailsFocused() {
		m.details.focused = false

		return m.refocused(), nil
	}

	held := m.hoveredDetails()
	if !m.isDetailsDrawn(held) {
		return m, nil
	}

	m.details.focused = true
	m.details.cursor = 0
	m.details.row = held.id

	return m.refocused(), nil
}

// refocused tells the screen the keys have moved between its list and the record under it, since
// a screen drawing its own pane has to repaint to say so.
func (m model) refocused() model {
	spec := m.screen.spec().details
	if spec == nil || spec.refocus == nil {
		return m
	}

	spec.refocus(&m)

	return m
}

func (m model) updateDetails(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	held := m.hoveredDetails()

	if !m.isDetailsDrawn(held) {
		m.details.focused = false

		return m.refocused(), nil
	}

	cursor := m.detailsCursor(held)

	switch {
	case isBound(k, m.keys.Details.Focus), k == "esc":
		m.details.focused = false

		return m.refocused(), nil
	case isBound(k, m.quitKeys()):
		return m, m.hide()
	case isBound(k, m.keys.Nav.Up):
		m = m.setDetailsCursor(held, cursor-1)
	case isBound(k, m.keys.Nav.Down):
		m = m.setDetailsCursor(held, cursor+1)
	case isBound(k, m.keys.Nav.Top):
		m = m.setDetailsCursor(held, 0)
	case isBound(k, m.keys.Nav.Bottom):
		m = m.setDetailsCursor(held, len(held.fields)-1)
	case isBound(k, m.keys.Details.Copy):
		return m.copyDetailsField(held, cursor)
	}

	return m, nil
}

func (m model) setDetailsCursor(held details, idx int) model {
	m.details.cursor = clampCursor(idx, len(held.fields))
	m.details.row = held.id

	return m
}

func (m model) quitKeys() []string {
	spec := m.screen.spec().details
	if spec == nil {
		return m.keys.Agents.Quit
	}

	return spec.quit(m.keys)
}

func (m model) copyDetailsField(held details, idx int) (tea.Model, tea.Cmd) {
	m.details.flashed = idx + 1

	return m, tea.Batch(m.copyCmd(held.fields[idx].value), flashRevertTick())
}

func (m model) detailsLines() int {
	if m.height <= 0 {
		return 0
	}

	lines := int(m.detailsHeight)
	if m.detailsHeight < 1 {
		lines = int(float64(m.height) * m.detailsHeight)
	}

	return min(lines, max(m.height/2-detailsChromeLines, 0))
}

const detailsChromeLines = 4

func (m model) detailsView() string {
	held := m.hoveredDetails()
	if !m.isDetailsDrawn(held) {
		return ""
	}

	// Two field lines are kept back rather than one, since renderList only says how many fields
	// it left out when it has room for that line and a field under it.
	notices := m.detailsNotices(held, m.detailsLines()-3)
	block, lines := "", m.detailsLines()
	if len(notices) > 0 {
		block = strings.Join(notices, "\n") + "\n\n"
		lines -= len(notices) + 1
	}

	cursor := m.detailsCursor(held)
	rows := m.detailFieldRows(held.fields, cursor)

	return m.detailsRule() +
		m.detailsTitle(held) +
		block +
		padRows(renderList(rows, cursor, lines), lines) +
		m.detailsRule()
}

func (m model) detailsRule() string {
	width := m.paneWidth()
	if width <= 0 {
		return "\n"
	}

	return dimStyle.Render(strings.Repeat("─", width)) + "\n"
}

func (m model) detailsTitle(held details) string {
	title := held.title
	if held.kind != "" {
		title = held.kind + ": " + title
	}

	return titleStyleFor(m.details.focused).Render(clip(title, m.lineWidth())) + "\n\n"
}

// detailsNotices are the lines the marks on the row take above the fields, giving up a notice
// that does not fit rather than the fields under it, since the row still carries its glyph.
func (m model) detailsNotices(held details, budget int) []string {
	if budget <= 0 {
		return nil
	}

	drawn := []string{}
	for _, n := range held.notices {
		lines := m.noticeLines(n, 0)
		if len(drawn)+len(lines) > budget {
			// The first notice is cut short rather than given up, since at a size where no
			// sentence fits whole the glyph on its own leaves the reader with nothing.
			if len(drawn) > 0 {
				break
			}

			lines = m.noticeLines(n, budget)
		}

		drawn = append(drawn, lines...)
	}

	return drawn
}

// noticeLines is one notice as it is drawn: the glyph the row carries, then the sentence wrapped
// under it in the column the glyph left, cut off at a number of lines when it is given one.
func (m model) noticeLines(n notice, most int) []string {
	const indent = "    "

	wrapped := ansi.Wordwrap(n.text, max(m.lineWidth()-len(indent), 1), "")
	text := strings.Split(wrapped, "\n")
	if most > 0 && len(text) > most {
		text = text[:most]
		text[most-1] += "…"
	}

	lines := []string{"  " + lipgloss.NewStyle().Foreground(n.color).Render(n.icon) + " " + dimStyle.Render(text[0])}
	for _, line := range text[1:] {
		lines = append(lines, indent+dimStyle.Render(line))
	}

	return lines
}

func (m model) detailFieldRows(fields []recordField, cursor int) []string {
	labels := labelColumn(fields)
	rows := make([]string, 0, len(fields))

	const gutter = 2

	for i, field := range fields {
		prefix := "  "
		if i == cursor {
			prefix = cursorStyleFor(m.details.focused).Render("› ")
		}

		label := dimStyle.Render(padRight(field.label, labels))
		value := clip(field.value, max(m.lineWidth()-labels-gutter-1, 1))
		if m.details.flashed == i+1 {
			value = flashFieldStyle.Render(value)
		}

		rows = append(rows, prefix+label+" "+value)
	}

	return rows
}

func (m model) lineWidth() int {
	if m.paneWidth() <= 0 {
		return nameWidth
	}

	return m.paneWidth()
}

func labelColumn(fields []recordField) int {
	widest := 0
	for _, field := range fields {
		widest = max(widest, len([]rune(field.label)))
	}

	return widest
}

func padRight(text string, width int) string {
	pad := width - len([]rune(text))
	if pad <= 0 {
		return text
	}

	return text + strings.Repeat(" ", pad)
}

var flashFieldStyle = lipgloss.NewStyle().Background(colorAccent).Foreground(lipgloss.Color("0"))
