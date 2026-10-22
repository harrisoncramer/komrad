package paneltest

import (
	"regexp"
	"slices"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

const (
	// gutterCols is the blank column the panel renders left of its frame.
	gutterCols = 1
	// edgeCols is the edge the panel renders right of its frame while a pane stands beside it.
	edgeCols = 2
	// dividerGap is the blank column either side of the divider between the list and the block.
	dividerGap = 1
)

var emptyStyle = regexp.MustCompile(`(\x1b\[[0-9;]+m\x1b\[m)+$`)

// typedCursor is a character rendered in reverse, which is how a text cursor stands on one.
var typedCursor = regexp.MustCompile(`\x1b\[(?:[0-9]+;)*7(?:;[0-9]+)*m(.)\x1b\[m`)

var reviewCursorLine = regexp.MustCompile(`^[^ ]+:\d+`)

// ruleEdges are the joins a rule can meet.
const ruleEdges = glyph.Divider + glyph.TeeRight + glyph.Cross

var taggedRule = regexp.MustCompile(`^` + glyph.Rule + `+ [^` + ruleEdges + `]*[^` + glyph.Rule + ruleEdges + ` ] ` + glyph.Rule)

// listDivider is a rule the list renders among its own rows.
var listDivider = regexp.MustCompile(`^` + glyph.Rule + `+ (Triage|All Agents) ` + glyph.Rule)

// gutterToken is what a list's gutter holds: the focus, a glyph, a loader frame or a figure.
var gutterToken = regexp.MustCompile(`^(` + glyph.Focus + `|[\x{E000}-\x{F8FF}\x{F0000}-\x{FFFFD}\x{2800}-\x{28FF}]|[0-9][0-9.]*[KMGTkhdwmsy]?|—)$`)

// scrollCount is the line a scrolled list says how many rows lie past its edge on.
var scrollCount = regexp.MustCompile(`^\s*[` + glyph.Up + glyph.Down + `] [0-9]+ more`)

// listRowIndent is the most a row leaves blank before its first cell.
const listRowIndent = 16

// viewScreens is the screen each view's label in the trail names.
var viewScreens = map[string]screen.ID{
	"agents":    screen.Agents,
	"schedules": screen.Schedules,
	"documents": screen.Documents,
	"worktrees": screen.Worktrees,
}

// ListLine is one row of the list a frame renders: where it stands in the frame, the list's part
// of the line, and the row with the gutter every row shares cut off.
type ListLine struct {
	Index int
	List  string
	Row   string
}

// hasEdge reports whether every line of a rendered frame closes on the edge drawn beside a pane.
func hasEdge(lines []string, width int) bool {
	if width < edgeCols {
		return false
	}

	for _, line := range lines {
		edge := ansi.Strip(ansi.Cut(line, width-1, width))
		if edge != glyph.Divider && edge != glyph.TeeRight {
			return false
		}
	}

	return true
}

// findDivider is the column of the rule the frame draws between the list and the block, and false
// while no block is drawn.
func findDivider(lines []string) (int, bool) {
	counts := map[int]int{}
	for _, line := range lines {
		col := 0
		for _, r := range ansi.Strip(line) {
			if strings.ContainsRune(glyph.Divider+glyph.Cross+glyph.TeeRight+glyph.TeeLeft, r) {
				counts[col]++
			}
			col += ansi.StringWidth(string(r))
		}
	}

	best, most := 0, 0
	for col, count := range counts {
		if count > most || count == most && col < best {
			best, most = col, count
		}
	}
	if most == 0 || most*2 < len(lines) {
		return 0, false
	}

	return best, true
}

// ListWidth is the columns a frame renders its list in.
func ListWidth(frame string) int {
	lines := SplitLines(frame)
	if divider, ok := findDivider(lines); ok {
		return divider - dividerGap
	}

	return slices.Max(xslice.Map(lines, ansi.StringWidth))
}

// blockStart is the column a frame renders its block from, past the list and the divider.
func blockStart(frame string) int {
	divider, ok := findDivider(SplitLines(frame))
	if !ok {
		return 0
	}

	return divider + 1 + dividerGap
}

// SplitLines is the frame's lines.
func SplitLines(frame string) []string {
	return strings.Split(frame, "\n")
}

// BelowViews is the frame's lines under the logo, its header and any row of views, whole when no
// logo is rendered.
func BelowViews(frame string) []string {
	lines := SplitLines(frame)
	if len(lines) <= 4 || !strings.Contains(lines[0], glyph.Logo+" komrad") {
		return lines
	}
	if strings.Contains(lines[3], "agents  schedules") {
		return lines[4:]
	}

	return lines[3:]
}

// Trail is the steps the frame's first line runs down to the row or page the cursor is on.
func Trail(frame string) string {
	lines := SplitLines(frame)
	if len(lines) == 0 {
		return ""
	}
	first, _, _ := strings.Cut(lines[0], glyph.Divider)
	first = strings.TrimSpace(first)
	if trail, isLogo := strings.CutPrefix(first, glyph.Logo+" komrad"); isLogo {
		return strings.TrimPrefix(trail, " "+glyph.Crumb+" ")
	}

	return first
}

// Screen is the screen the frame's trail names.
func Screen(frame string) screen.ID {
	trail, _, _ := strings.Cut(Trail(frame), " "+glyph.Separator+" ")
	crumbs := strings.Split(trail, " "+glyph.Crumb+" ")
	last := crumbs[len(crumbs)-1]

	switch {
	case last == "review":
		return screen.Review
	case last == "status checks":
		return screen.StatusChecks
	case last == "logs" && len(crumbs) > 1 && crumbs[len(crumbs)-2] == "status checks":
		return screen.StatusCheckLogs
	case last == "logs":
		return screen.Logs
	}

	return viewScreens[crumbs[0]]
}

// ListRows is the rows of the list the frame renders, each cut past the gutter.
func ListRows(frame string) []string {
	return xslice.Map(ListLines(frame), func(line ListLine) string { return line.Row })
}

// HoveredRow is the list row the cursor stands on.
func HoveredRow(frame string) string {
	for _, line := range ListLines(frame) {
		if strings.HasPrefix(line.List, glyph.Focus) {
			return line.Row
		}
	}

	width := ListWidth(frame)
	for _, line := range SplitLines(frame) {
		if strings.HasPrefix(line, glyph.Focus) {
			return strings.TrimSpace(strings.TrimPrefix(ansi.Truncate(line, width, ""), glyph.Focus))
		}
	}

	return ""
}

// ListLines reads the rows between the search prompt and the first rule the list does not
// render itself, cutting them all at the leftmost column a name opens at.
func ListLines(frame string) []ListLine {
	lines := SplitLines(frame)
	width := ListWidth(frame)
	start := slices.IndexFunc(lines, func(line string) bool {
		prompt := strings.TrimSpace(ansi.Truncate(line, width, ""))

		return strings.HasPrefix(prompt, "/") || strings.HasPrefix(prompt, glyph.Defaults().Spell(glyph.Filter))
	})
	if start < 0 {
		return nil
	}

	var found []ListLine
	for i := start + 1; i < len(lines); i++ {
		list := strings.TrimRight(ansi.Truncate(lines[i], width, ""), " ")
		if strings.HasPrefix(list, glyph.Rule) {
			if listDivider.MatchString(list) {
				continue
			}

			break
		}
		if !isListRow(list) {
			continue
		}

		found = append(found, ListLine{Index: i, List: list})
	}

	if len(found) == 0 {
		return nil
	}

	column := width
	for _, line := range found {
		column = min(column, findNameColumn(line.List))
	}

	for i := range found {
		found[i].Row = ansi.Cut(found[i].List, column, width)
	}

	return found
}

// isListRow reports whether a line opens on the focus column with a cell inside the gutter, which
// the line a list says over no rows and text set across the pane do not.
func isListRow(list string) bool {
	indent := len(list) - len(strings.TrimLeft(list, " "))

	return list != "" && !scrollCount.MatchString(list) && indent <= listRowIndent && (indent > 0 || strings.HasPrefix(list, glyph.Focus))
}

// findNameColumn is the column a row's name opens at, past every token of the gutter.
func findNameColumn(list string) int {
	at := 0
	for _, field := range strings.SplitAfter(list, " ") {
		token := strings.TrimSpace(field)
		if token != "" && !gutterToken.MatchString(token) {
			return at
		}

		at += ansi.StringWidth(field)
	}

	return at
}

// RawListRow is the styled line of the list row holding name.
func RawListRow(frame, name string) string {
	lines := SplitLines(frame)
	for _, line := range ListLines(ansi.Strip(frame)) {
		if strings.Contains(line.Row, name) {
			return ansi.Truncate(lines[line.Index], ListWidth(ansi.Strip(frame)), "")
		}
	}

	return ""
}

// Record is the lines of the record the frame renders between its first two rules.
func Record(frame string) []string {
	record, _ := cutRecord(frame)

	return record
}

// IsRecordRendered reports whether the frame renders a record.
func IsRecordRendered(frame string) bool {
	_, rendered := cutRecord(frame)

	return rendered
}

// CutStyled is a styled line cut to width, without the empty styles left at its end.
func CutStyled(line string, width int) string {
	return emptyStyle.ReplaceAllString(ansi.Truncate(line, width, ""), "")
}

// RuleWidth is the columns a rule runs across before it meets the divider.
func RuleWidth(line string) int {
	if tagged := taggedRule.FindString(line); tagged != "" {
		line = strings.Repeat(glyph.Rule, ansi.StringWidth(tagged)) + line[len(tagged):]
	}
	rest := strings.TrimLeft(line, glyph.Rule)
	width := ansi.StringWidth(line) - ansi.StringWidth(rest)
	if strings.HasPrefix(rest, glyph.TeeRight) || strings.HasPrefix(rest, glyph.Cross) {
		width--
	}

	return width
}

func cutRecord(frame string) ([]string, bool) {
	var record []string
	width, rules := 0, 0
	for _, line := range SplitLines(frame) {
		if strings.HasPrefix(line, glyph.Rule) {
			rules++
			if rules == 2 {
				break
			}
			width = RuleWidth(line)

			continue
		}
		if rules == 1 {
			record = append(record, strings.TrimRight(ansi.Truncate(line, width, ""), " "))
		}
	}
	if rules < 2 {
		return nil, false
	}
	if len(record) == 0 {
		return nil, true
	}
	if !strings.HasPrefix(record[0], "  ") && !strings.HasPrefix(record[0], glyph.Focus+" ") {
		return nil, false
	}

	return record, true
}

// RecordField is the value the record renders beside a label.
func RecordField(frame, label string) string {
	for _, line := range Record(frame) {
		field := strings.TrimLeft(line, glyph.Focus+" ")
		if value, isField := strings.CutPrefix(field, label+" "); isField {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

// RecordCursor is the record field the cursor stands on.
func RecordCursor(frame string) string {
	for _, line := range Record(frame) {
		if field, isHovered := strings.CutPrefix(line, glyph.Focus+" "); isHovered {
			return field
		}
	}

	return ""
}

// RecordLine is the frame line the record renders a label on, -1 where it renders none.
func RecordLine(frame, label string) int {
	for i, line := range SplitLines(frame) {
		if strings.HasPrefix(strings.TrimLeft(line, glyph.Focus+" "), label+" ") {
			return i
		}
	}

	return -1
}

// Legend is the lines under the frame's last rule.
func Legend(frame string) string {
	rows := SplitLines(frame)
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.HasPrefix(rows[i], glyph.Rule) {
			return strings.Join(rows[i+1:], "\n")
		}
	}

	return ""
}

// Sidebar is the frame's lines under the views, cut to the list.
func Sidebar(frame string) []string {
	width := ListWidth(frame)

	return xslice.Map(BelowViews(frame), func(line string) string {
		return strings.TrimRight(ansi.Truncate(line, width, ""), " ")
	})
}

// SidebarProse is the sidebar's words joined on single spaces.
func SidebarProse(frame string) string {
	return JoinProse(Sidebar(frame))
}

// Popup is the rows inside the review's popup, empty while none is rendered.
func Popup(frame string) []string {
	lines := SplitLines(frame)
	for top, line := range lines {
		start, end := strings.Index(line, "╭"), strings.Index(line, "╮")
		if start < 0 || end < 0 {
			continue
		}

		left, right := ansi.StringWidth(line[:start]), ansi.StringWidth(line[:end])+1
		var rows []string
		for _, row := range lines[top+1:] {
			cell := ansi.Cut(row, left, right)
			if strings.HasPrefix(cell, "╰") {
				return rows
			}

			rows = append(rows, strings.TrimSpace(strings.Trim(cell, glyph.Divider)))
		}

		return rows
	}

	return nil
}

// PopupMiddle is the cell at the middle of the review's popup in a frame rendered whole.
func PopupMiddle(frame string) (int, int) {
	lines := SplitLines(frame)
	for top, line := range lines {
		start := strings.Index(line, "╭")
		if start < 0 {
			continue
		}

		left, width := ansi.StringWidth(line[:start]), ansi.StringWidth(line[start:strings.Index(line, "╮")])
		height := slices.IndexFunc(lines[top:], func(row string) bool { return strings.Contains(row, "╰") })

		return left + width/2, top + height/2
	}

	return -1, -1
}

// PopupProse is the popup's words joined on single spaces.
func PopupProse(frame string) string {
	return JoinProse(Popup(frame))
}

// JoinProse is the words of lines joined on single spaces.
func JoinProse(lines []string) string {
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

// Stage is the frame's lines cut to the block beside the list.
func Stage(frame string) []string {
	start := blockStart(frame)

	return xslice.Map(SplitLines(frame), func(line string) string {
		return strings.TrimRight(ansi.TruncateLeft(line, start, ""), " ")
	})
}

// StageText is Stage joined into one string by newlines.
func StageText(frame string) string {
	return strings.Join(Stage(frame), "\n")
}

// ReviewCursor is the review's line the cursor stands on, file and line first.
func ReviewCursor(frame string) string {
	for _, line := range Sidebar(frame) {
		if reviewCursorLine.MatchString(line) {
			return line
		}
	}

	return ""
}

// ClickOn is a click on text where the frame first renders it.
func (p *Panel) ClickOn(text string) tea.MouseClickMsg {
	p.t.Helper()

	return p.ClickOnLine(text, text)
}

// ClickOnLine is a click on text on the first line of the frame holding line.
func (p *Panel) ClickOnLine(line, text string) tea.MouseClickMsg {
	p.t.Helper()

	for y, row := range SplitLines(p.Frame()) {
		if !strings.Contains(row, line) {
			continue
		}

		if at := strings.Index(row, text); at >= 0 {
			return tea.MouseClickMsg{
				X:      gutterCols + ansi.StringWidth(row[:at]),
				Y:      y,
				Button: tea.MouseLeft,
			}
		}
	}

	require.Failf(p.t, "no text in the frame", "%q on a line holding %q", text, line)

	return tea.MouseClickMsg{}
}

// ClickOnLineStart is a click just past prefix on the first line of the frame opening with it.
func (p *Panel) ClickOnLineStart(prefix string) tea.MouseClickMsg {
	p.t.Helper()

	for y, row := range SplitLines(p.Frame()) {
		if strings.HasPrefix(row, prefix) {
			return tea.MouseClickMsg{
				X:      gutterCols + ansi.StringWidth(prefix) + 1,
				Y:      y,
				Button: tea.MouseLeft,
			}
		}
	}

	require.Failf(p.t, "no line in the frame", "a line starting %q", prefix)

	return tea.MouseClickMsg{}
}
