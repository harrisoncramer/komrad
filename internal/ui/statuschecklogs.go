package ui

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/reader"
	"komrad/internal/view"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// statusCheckLogChunk is the most lines one read of a check's log asks for.
const statusCheckLogChunk = 5000

// statusCheckLogErrorMarker is the prefix GitHub Actions puts at the start of a log line the job reported as an error.
const statusCheckLogErrorMarker = "##[error]"

const (
	statusCheckLogSearchDebounce  = 150 * time.Millisecond
	statusCheckLogSearchMinLength = 3
)

// statusCheckLogsScreen is the logs of the checks the status checks screen lists, joined into one document.
type statusCheckLogsScreen struct {
	rowCursor
	agentID string
	// openedFrom is the screen the logs were opened from, which going back returns to.
	openedFrom screen
	// generation tells this visit's reads apart from an earlier one's.
	generation int
	logs       map[string]*statusCheckLogRead
	drawn      statusCheckLogsDrawn
	// placed is the row of the document the hovered row's header was last put on, and placedTop
	// the row the window was scrolled to with it.
	placed    int
	placedTop int
	// checks is the status checks screen whose scope the logs are joined under.
	checks *statusChecksScreen
}

// statusCheckLogRead is the lines of one check's log read so far.
type statusCheckLogRead struct {
	lines   []string
	err     error
	reading bool
}

// statusCheckLogsDrawn is the document as last drawn, kept while nothing it was drawn from has changed.
type statusCheckLogsDrawn struct {
	shape   string
	rows    []string
	anchors map[string]int
}

// statusCheckLogRow is a check, or one step of its log, in the list beside the document.
type statusCheckLogRow struct {
	item statusCheckItem
	step *pullrequestv1.CheckLogStep
}

// key names the row, a status check by its id or one step by the status check id and step number, so the cursor and the document anchors find it.
func (r statusCheckLogRow) key() string {
	if r.step != nil {
		return "status-check-log:" + r.item.checkID + "/" + strconv.Itoa(int(r.step.GetNumber()))
	}

	return "status-check-log:" + r.item.checkID
}

func statusCheckLogRowName(row statusCheckLogRow) string {
	if row.step != nil {
		return row.step.GetName()
	}

	return row.item.name
}

// statusCheckLogReadMsg is one chunk of a check's log come back.
type statusCheckLogReadMsg struct {
	generation int
	checkID    string
	resp       *pullrequestv1.ReadCheckLogResponse
	err        error
}

func (c *statusCheckLogsScreen) enter(m *model, target screenTarget) tea.Cmd {
	generation := c.generation + 1
	if target.agentID != "" {
		*c = statusCheckLogsScreen{
			agentID:    target.agentID,
			openedFrom: target.from,
		}
	}
	c.generation, c.logs, c.placed, c.placedTop = generation, map[string]*statusCheckLogRead{}, 0, 0

	return tea.Batch(m.readStatusChecksCmd(), c.readsCmd(m))
}

func (c *statusCheckLogsScreen) leaveScreen(*model) tea.Cmd {
	c.generation++

	return nil
}

func (c *statusCheckLogsScreen) update(m *model, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.list().update(m, key, c.listActions())
}

func (c *statusCheckLogsScreen) list() screenList[statusCheckLogRow] {
	return screenList[statusCheckLogRow]{
		cursor: func() *rowCursor { return &c.rowCursor },
		rows:   c.rows,
		name:   statusCheckLogRowName,
		moved: func(m *model) tea.Cmd {
			c.anchor(m)

			return nil
		},
	}
}

// shown is the checks the status checks screen lists under its scope.
func (c *statusCheckLogsScreen) shown(m *model) []statusCheckItem {
	return xslice.Filter(m.statusChecks.commit.items, m.statusChecks.shownScope().holds)
}

func (c *statusCheckLogsScreen) rows(m *model) []statusCheckLogRow {
	var rows []statusCheckLogRow
	for _, item := range c.shown(m) {
		rows = append(rows, statusCheckLogRow{item: item})
		rows = append(rows, xslice.Map(item.log.GetSteps(), func(step *pullrequestv1.CheckLogStep) statusCheckLogRow {
			return statusCheckLogRow{item: item, step: step}
		})...)
	}

	return narrowed(m.filter, narrowedParams[statusCheckLogRow]{
		rows: rows,
		depth: func(r statusCheckLogRow) int {
			if r.step != nil {
				return 1
			}

			return 0
		},
		name: statusCheckLogRowName,
	})
}

// anchor puts the hovered row's header at the top of the document.
func (c *statusCheckLogsScreen) anchor(m *model) {
	split, ok := m.screenColumnSplit()
	if !ok {
		return
	}

	_, anchors := c.docAnchors(m, split.column)
	line, ok := anchors[c.list().hovered(m)]
	if !ok {
		return
	}

	m.reading.tail = false
	if rows, height, ok := m.docLayout(); ok {
		m.reading.Line, c.placed = line, line
		m.reading.Top = reader.Window{Len: len(rows), Markers: true}.Clamp(line, height)
		c.placedTop = m.reading.Top
	}
}

// reanchor keeps the hovered row's header in view as the document changes above it, unless the reader has moved or scrolled off it.
func (c *statusCheckLogsScreen) reanchor(m *model) {
	if m.screen != screenStatusCheckLogs || m.reading.focused || m.reading.Line != c.placed || m.reading.Top != c.placedTop {
		return
	}

	c.anchor(m)
}

func (c *statusCheckLogsScreen) view(m *model) string {
	return m.columnListView(c.listParams(m))
}

func (c *statusCheckLogsScreen) listParams(m *model) screenListParams {
	return screenListParams{
		tally:    m.statusChecks.commit.tally(),
		help:     c.help,
		empty:    m.statusChecks.emptyLine(m),
		rows:     len(c.rows(m)),
		snapshot: func(pane listPane) view.Snapshot { return c.snapshot(m, pane) },
	}
}

func (c *statusCheckLogsScreen) snapshot(m *model, pane listPane) view.Snapshot {
	rows := c.rows(m)

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r statusCheckLogRow) view.Row {
			name := statusCheckLogRowName(r)
			row := view.Row{
				Key:     r.key(),
				Kind:    view.KindResult,
				Name:    name,
				Matches: matchesIn(m.filter.pattern, name),
				Process: view.ProcessNone,
				Cells:   []view.Cell{statusCheckIcon(r.item, m.loading.frame())},
			}
			if r.step != nil {
				row.Kind = view.KindChild
				row.Cells = []view.Cell{statusCheckLogStepIcon(r.step)}
			}

			return row
		}),
		Caret:      pane.caret,
		BlinkedOut: pane.blinkedOut,
		Focused:    rowKeyAt(rows, c.cursor),
		Pane:       pane.width,
		Columns:    statusCheckColumns(),
	}
}

// statusCheckLogStepIcon is the icon of a step of a log, blank for a step that reported no conclusion.
func statusCheckLogStepIcon(step *pullrequestv1.CheckLogStep) view.Cell {
	if step.GetConclusion() == "" {
		return view.Cell{}
	}

	return statusCheckIcon(statusCheckItem{
		state:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
		conclusion: step.GetConclusion(),
	}, "")
}

func (c *statusCheckLogsScreen) kinds() stops {
	if c.checks == nil {
		return stops{}
	}

	return c.checks.kinds()
}

func (c *statusCheckLogsScreen) kindsFocused() *bool {
	if c.checks == nil {
		return new(bool)
	}

	return c.checks.kindsFocused()
}

func (c *statusCheckLogsScreen) kindsFit(m *model) bool {
	return m.listFitsTabs(c.listParams(m))
}

func (c *statusCheckLogsScreen) kindsSwitch(*model) bool {
	return true
}

// stepKind moves the status checks screen to another scope, which is the scope the logs are joined under.
func (c *statusCheckLogsScreen) stepKind(m *model, delta int) tea.Cmd {
	hovered := c.list().hovered(m)
	cmd := m.statusChecks.stepKind(m, delta)
	c.list().place(m, hovered)
	c.reanchor(m)

	return tea.Batch(cmd, c.readsCmd(m))
}

func (c *statusCheckLogsScreen) listActions() screenActionSpec {
	return screenActionSpec{
		table: append(c.list().actions(),
			screenAction{
				id:   actionStatusCheckLogsBack,
				keys: fixedBack,
				nav: func(m *model, _ string) tea.Cmd {
					return m.enter(screenStatusChecks, screenTarget{})
				},
			},
		),
		legend: slices.Concat(screenNavLegend(), []screenLegendRow{
			{
				of:    []actionID{actionStatusCheckLogsBack},
				desc:  func(*model) string { return "back" },
				strip: true,
			},
		}),
	}
}

func (c *statusCheckLogsScreen) help(m *model) keyMap {
	return c.listActions().help(m)
}

func (c *statusCheckLogsScreen) parent() screen {
	return c.openedFrom
}

func (c *statusCheckLogsScreen) crumbs(m *model) crumbs {
	name := c.agentID
	if item, ok := m.listing.anyAgent(c.agentID); ok {
		name = m.listing.agentName(item.Agent)
	}

	return crumbs{trail: []string{name, "status checks", "logs"}}
}

func (c *statusCheckLogsScreen) doesDrawFromListing() bool {
	return true
}

func (c *statusCheckLogsScreen) takesEveryKey() bool {
	return false
}

func (c *statusCheckLogsScreen) column() columnSpec {
	return columnSpec{
		floor:         columnFloor,
		read:          isAlwaysRead,
		searchOptions: []filterOption{withDebounce(statusCheckLogSearchDebounce), withMinLength(statusCheckLogSearchMinLength)},
		docRows: func(m *model, width int) []string {
			rows, _ := m.statusCheckLogs.docAnchors(m, width)

			return rows
		},
	}
}

func (c *statusCheckLogsScreen) stageResting(*model) tea.Cmd {
	return nil
}

func (c *statusCheckLogsScreen) dispatches(*model) []dispatching {
	return []dispatching{c.listActions()}
}

var (
	_ kindSwitcher    = (*statusCheckLogsScreen)(nil)
	_ listFilterer    = (*statusCheckLogsScreen)(nil)
	_ crumbDrawer     = (*statusCheckLogsScreen)(nil)
	_ loadingReporter = (*statusCheckLogsScreen)(nil)
	_ screenLeaver    = (*statusCheckLogsScreen)(nil)
	_ docNamer        = (*statusCheckLogsScreen)(nil)
)

func (c *statusCheckLogsScreen) hoveredKey(m *model) string {
	return c.list().hovered(m)
}

func (c *statusCheckLogsScreen) placeAt(m *model, key string) {
	c.list().place(m, key)
}

func (c *statusCheckLogsScreen) filterRows(m *model) []filterRow {
	return c.list().filterRows(m)
}

// docName names the one document every row of the list reads, so moving the cursor keeps the reader's place.
func (c *statusCheckLogsScreen) docName(*model) string {
	return c.agentID + "/status-check-logs"
}

func (c *statusCheckLogsScreen) isLoading(m *model) bool {
	if m.statusChecks.listing {
		return true
	}

	for _, read := range c.logs {
		if read.reading {
			return true
		}
	}

	return false
}

// docAnchors is the logs of the shown checks drawn at a width, with the row each list row's header is drawn on.
func (c *statusCheckLogsScreen) docAnchors(m *model, width int) ([]string, map[string]int) {
	shown := c.shown(m)
	shape := c.docShape(shown, width)
	if c.drawn.shape == shape {
		return c.drawn.rows, c.drawn.anchors
	}

	indent := strings.Repeat(" ", docMargin())
	wrap := max(width-2*docMargin(), 1)
	var rows []string
	anchors := map[string]int{}
	line := func(text string, style lipgloss.Style) {
		rows = append(rows, xslice.Map(strings.Split(ansi.Wrap(text, wrap, ""), "\n"), func(part string) string {
			return fitRow(indent+style.Render(part), width)
		})...)
	}

	for i, item := range shown {
		if i > 0 {
			rows = append(rows, "")
		}

		anchors[statusCheckLogRow{item: item}.key()] = len(rows)
		title := "═ " + item.name + " "
		rows = append(rows, fitRow(indent+lipgloss.NewStyle().Bold(true).Render(title+strings.Repeat("═", max(wrap-lipgloss.Width(title), 0))), width))

		if notice := c.logNotice(item); notice != "" {
			line(notice, dimStyle())

			continue
		}

		steps := slices.SortedStableFunc(slices.Values(item.log.GetSteps()), func(a, b *pullrequestv1.CheckLogStep) int {
			return cmp.Compare(a.GetFirstLine(), b.GetFirstLine())
		})
		header := func(upTo int) {
			for len(steps) > 0 && int(steps[0].GetFirstLine()) <= upTo {
				anchors[statusCheckLogRow{item: item, step: steps[0]}.key()] = len(rows)
				line(glyph.Step+" "+steps[0].GetName(), lipgloss.NewStyle().Bold(true))
				steps = steps[1:]
			}
		}

		read := c.logs[item.checkID]
		for n, text := range read.lines {
			header(n + 1)

			style := lipgloss.NewStyle()
			if strings.HasPrefix(text, statusCheckLogErrorMarker) {
				style = paint.Panel.Style(paint.ToneDanger)
			}
			line(strings.ReplaceAll(text, "\t", "    "), style)
		}
		if !read.reading {
			header(math.MaxInt)
		}
	}

	c.drawn = statusCheckLogsDrawn{shape: shape, rows: rows, anchors: anchors}

	return rows, anchors
}

// docShape names everything the document is drawn from, so it is drawn again only once that changes.
func (c *statusCheckLogsScreen) docShape(shown []statusCheckItem, width int) string {
	var shape strings.Builder
	fmt.Fprintf(&shape, "%d/%d", c.generation, width)
	for _, item := range shown {
		read := c.logs[item.checkID]
		lines, reading, failed := 0, false, false
		if read != nil {
			lines, reading, failed = len(read.lines), read.reading, read.err != nil
		}
		fmt.Fprintf(&shape, "/%s:%d:%d:%t:%t", item.checkID, item.log.GetState(), lines, reading, failed)
	}

	return shape.String()
}

// logNotice is what is drawn under a check in place of its log, empty while there are lines to draw.
func (c *statusCheckLogsScreen) logNotice(item statusCheckItem) string {
	read := c.logs[item.checkID]
	switch {
	case read != nil && read.err != nil:
		return "could not read: " + errui.GetReason(read.err)
	case read != nil && len(read.lines) > 0:
		return ""
	case read != nil && read.reading:
		return "reading…"
	}

	switch item.log.GetState() {
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED:
		return "empty log"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_PENDING:
		return "log not fetched yet"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_TOO_LARGE:
		return "log too large to keep"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_MISSING:
		return "the forge has no log for this check"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_EXPIRED:
		return "log expired on the forge"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_FAILED:
		return "log could not be fetched"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_UNSPECIFIED, pullrequestv1.CheckLogState_CHECK_LOG_STATE_NONE:
	}

	return "no log"
}

// readsCmd starts reading the log of each shown check that has one kept and is not read yet.
func (c *statusCheckLogsScreen) readsCmd(m *model) tea.Cmd {
	if m.screen != screenStatusCheckLogs || c.logs == nil {
		return nil
	}

	var cmds []tea.Cmd
	for _, item := range c.shown(m) {
		if item.log.GetState() != pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED || item.log.GetLineCount() == 0 {
			continue
		}
		if _, ok := c.logs[item.checkID]; ok {
			continue
		}

		c.logs[item.checkID] = &statusCheckLogRead{reading: true}
		cmds = append(cmds, c.readCmd(m, item.checkID, 1, item.log.GetLineCount()))
	}

	return tea.Batch(cmds...)
}

// readCmd reads one chunk of a check's log, from its first line toward its last.
func (c *statusCheckLogsScreen) readCmd(m *model, checkID string, first, last int32) tea.Cmd {
	generation, clients := c.generation, m.api
	count := min(last-first+1, statusCheckLogChunk)

	return func() tea.Msg {
		resp, err := clients.readCheckLog(checkID, first, count)

		return statusCheckLogReadMsg{generation: generation, checkID: checkID, resp: resp, err: err}
	}
}

func (m *model) applyStatusCheckLogRead(msg statusCheckLogReadMsg) tea.Cmd {
	c := &m.statusCheckLogs
	read, ok := c.logs[msg.checkID]
	if msg.generation != c.generation || !ok {
		return nil
	}

	if msg.err != nil {
		read.err, read.reading = msg.err, false
		c.reanchor(m)

		return nil
	}

	read.lines = append(read.lines, xslice.Map(msg.resp.GetLines(), func(line *pullrequestv1.CheckLogLine) string { return line.GetText() })...)
	total := msg.resp.GetLineCount()
	var next tea.Cmd
	if int32(len(read.lines)) >= total || len(msg.resp.GetLines()) == 0 {
		read.reading = false
	} else {
		next = c.readCmd(m, msg.checkID, int32(len(read.lines))+1, total)
	}
	c.reanchor(m)

	return next
}
