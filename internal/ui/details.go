package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The details section, the block under a list that draws what the row above it holds. It is
// part of the panel's own view rather than a pane of its own, so what is in it can be
// scrolled, pointed at, and copied with the same keys as everything else here.
//
// Every screen that lists something draws one, and each says what its own hovered row holds.
// The section itself is shared: one cursor, one focus, one set of keys, whichever list is
// above it.

// detailsState is the section's own cursor and whether it has the keys. The list keeps its
// cursor while the section holds them, so the row being read stays marked as the field
// cursor moves down its fields.
type detailsState struct {
	focused bool
	cursor  int
	// row is the id of the record the cursor was last moved under. The cursor starts again
	// at the top when another row is hovered, since the field it was on belonged to that one,
	// and this is read rather than reset per keypress so a refresh that moves rows about
	// cannot leave the cursor pointing into a record it was never set against.
	row string
	// flashed is the 1-based field whose value was just copied, 0 for none. It reverts on
	// a tick, which is the only sign the copy key registered.
	flashed int
}

// details is what the section draws: a title and the fields under it. A row with nothing
// to say returns no fields, which is what closes the section and drops the focus.
type details struct {
	// id is what the record belongs to, so the cursor can tell a new row from a redraw of
	// the one it is already on.
	id string
	// kind is what sort of thing the record describes, which the title leads with: the same
	// name can be an agent on one row and the workflow it sits under on another.
	kind   string
	title  string
	fields []recordField
}

// hoveredDetails is what the row under the cursor of the list on screen holds. A screen that
// lists nothing with fields of its own returns none, which closes the section.
func (m model) hoveredDetails() details {
	switch m.screen {
	case screenAgents:
		return m.agentsDetails()
	case screenSchedules:
		return m.schedulesDetails()
	case screenArchive:
		return m.archiveDetails()
	default:
		return details{}
	}
}

// detailsScreen reports whether the screen on show draws a details section, which is every
// screen that lists rows carrying fields. The search screen and the actions list are the two
// that do not: a hit is a line of a conversation, and an action is its own description.
func (m model) detailsScreen() bool {
	switch m.screen {
	case screenAgents, screenSchedules, screenArchive:
		return true
	default:
		return false
	}
}

// detailsCursor is the field the section points at: the top of a record the cursor was not
// set against, and otherwise held inside the fields that record actually has. A refresh can
// shorten one under a cursor sitting past its new end, an agent that dropped a metadata key
// being the ordinary way.
func (m model) detailsCursor(held details) int {
	if m.details.row != held.id {
		return 0
	}

	return min(m.details.cursor, max(len(held.fields)-1, 0))
}

// toggleDetailsFocus hands the keys to the section, or takes them back. A row with no fields
// has nothing to point at, so the key does nothing there rather than focusing an empty block.
func (m model) toggleDetailsFocus() (tea.Model, tea.Cmd) {
	if m.details.focused {
		m.details.focused = false

		return m, nil
	}

	held := m.hoveredDetails()
	if len(held.fields) == 0 {
		return m, nil
	}

	m.details.focused = true
	m.details.cursor = 0
	m.details.row = held.id

	return m, nil
}

// updateDetails handles the keys while the section holds them. Movement runs down the fields
// rather than the rows, and the keys that act on an agent stand down, since the cursor above
// is not what is being pointed at.
func (m model) updateDetails(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	held := m.hoveredDetails()

	// A record that emptied under the cursor, which a refresh can do, leaves nothing to hold
	// the keys for.
	if len(held.fields) == 0 {
		m.details.focused = false

		return m, nil
	}

	m.details.row = held.id

	switch {
	// esc is taken as a literal here rather than through a screen's own back binding, which
	// also carries left and h: while the section holds the keys those are movement.
	case isBound(k, m.keys.Details.Focus), k == "esc":
		m.details.focused = false
	case isBound(k, m.quitKeys()):
		return m, m.hide()
	case isBound(k, m.keys.Nav.Up):
		m.details.cursor = max(m.detailsCursor(held)-1, 0)
	case isBound(k, m.keys.Nav.Down):
		m.details.cursor = min(m.detailsCursor(held)+1, len(held.fields)-1)
	case isBound(k, m.keys.Nav.Top):
		m.details.cursor = 0
	case isBound(k, m.keys.Nav.Bottom):
		m.details.cursor = len(held.fields) - 1
	case isBound(k, m.keys.Details.Copy):
		return m.copyDetailsField(held)
	}

	return m, nil
}

// quitKeys is the binding that closes the panel from the screen on show. Each screen binds
// its own, so the section asks which list it is under rather than answering the agents
// list's key from wherever it is drawn.
func (m model) quitKeys() []string {
	switch m.screen {
	case screenSchedules:
		return m.keys.Schedules.Quit
	case screenArchive:
		return m.keys.Archive.Quit
	default:
		return m.keys.Agents.Quit
	}
}

// copyDetailsField copies the value the cursor names and flashes its line while the write
// runs, the panel's only answer to a key that otherwise changes nothing on screen.
func (m model) copyDetailsField(held details) (tea.Model, tea.Cmd) {
	idx := m.detailsCursor(held)
	m.details.flashed = idx + 1

	return m, tea.Batch(m.copyCmd(held.fields[idx].value), flashRevertTick())
}

// detailsLines is how many field lines the section draws, from the share of the pane the
// layout gives it. The chrome around the fields is added on top of this rather than paid for
// out of it, and the two together never take more than half the pane, so the list above
// always has room to be moved around in, however tall a record under the cursor is. A pane
// too short to fit the chrome and one field gets none at all, since a list with nowhere to
// stand is worse than a record that has to be read by moving the cursor.
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

// detailsChromeLines is what the section spends on everything that is not a field: the rule
// that separates it from the list, the title, the blank line under the title, and the rule
// that closes it off from the legend below.
const detailsChromeLines = 4

// detailsView draws the section under the list: a rule, the row's title, then its fields,
// clipped to the share of the pane the layout gives it. Fields past that share are windowed
// around the cursor the way the list itself is, so a long record is read by moving down it.
//
// The block ends in the newline every block the panel builds ends in, so the list above it
// takes its height out of the same budget.
func (m model) detailsView() string {
	held := m.hoveredDetails()
	if len(held.fields) == 0 {
		return ""
	}

	lines := m.detailsLines()
	if lines < 1 {
		return ""
	}

	cursor := m.detailsCursor(held)
	rows := m.detailFieldRows(held.fields, cursor)

	// The rules and the title are chrome drawn around the fields, on top of the height
	// budgeted for the fields themselves rather than out of it.
	return m.detailsRule() +
		m.detailsTitle(held) +
		renderList(rows, cursor, lines) +
		m.detailsRule()
}

// detailsRule is the line the section is drawn between, which separates it from the list above
// and the legend below. It is drawn to the pane's width; a pane that has not been measured yet
// gets a rule of nothing rather than one of a guessed width.
func (m model) detailsRule() string {
	if m.width <= 0 {
		return "\n"
	}

	return dimStyle.Render(strings.Repeat("─", m.width)) + "\n"
}

// detailsTitle names what the fields below belong to, led by what sort of thing it is, in the
// accent while the section holds the keys so which half of the sidebar a keypress would reach
// is on screen. It ends in the blank line a list's own title stands above.
func (m model) detailsTitle(held details) string {
	title := held.title
	if held.kind != "" {
		title = held.kind + ": " + title
	}

	return titleStyleFor(m.details.focused).Render(clip(title, m.lineWidth())) + "\n\n"
}

// detailFieldRows draws one line per field: its label, then its value in the room left over.
// The value is clipped rather than wrapped, since the whole of it is a copy key away and a
// wrapped URL would cost the fields below it their lines.
func (m model) detailFieldRows(fields []recordField, cursor int) []string {
	labels := labelColumn(fields)
	rows := make([]string, 0, len(fields))

	// The gutter is two columns whether it holds the cursor or not, so the labels beside it
	// stay in one line down the section.
	const gutter = 2

	for i, field := range fields {
		// The field the cursor is on stays marked while the list above holds the keys, the
		// way the row up there stays marked while these do, and gives up the accent to it.
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

// lineWidth is how much of a line the section has to draw into, which is the panel's own
// pane. Before the first resize has said what that is, a name's own width stands in.
func (m model) lineWidth() int {
	if m.width <= 0 {
		return nameWidth
	}

	return m.width
}

// labelColumn is how wide the labels are drawn, the longest of them, so the values line up
// down the section rather than starting wherever their own label ended.
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

// flashFieldStyle is the copy flash: the accent behind the value that was just copied, held
// for as long as the revert tick takes.
var flashFieldStyle = lipgloss.NewStyle().Background(colorAccent).Foreground(lipgloss.Color("0"))
