package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/reader"
	"komrad/internal/view"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// checkLogChunk is the most lines one read of a check's log asks for.
const checkLogChunk = 5000

// checkLogError opens a log line the job wrote as an error.
const checkLogError = "##[error]"

// checkLogsScreen is the logs of the checks the status checks screen lists, joined into one document.
type checkLogsScreen struct {
	rowCursor
	agentID string
	from    screen
	// generation tells this visit's reads apart from an earlier one's.
	generation int
	logs       map[string]*checkLogRead
	drawn      checkLogsDrawn
	// checks is the status checks screen whose scope the logs are joined under.
	checks *statusChecksScreen
}

// checkLogRead is the lines of one check's log read so far.
type checkLogRead struct {
	lines   []string
	err     error
	reading bool
}

// checkLogsDrawn is the document as last drawn, kept while nothing it was drawn from has changed.
type checkLogsDrawn struct {
	shape   string
	rows    []string
	anchors map[string]int
}

// checkLogRow is a check, or one step of its log, in the list beside the document.
type checkLogRow struct {
	item statusCheckItem
	step *pullrequestv1.CheckLogStep
}

func (r checkLogRow) key() string {
	if r.step != nil {
		return "check-log:" + r.item.checkID + "/" + strconv.Itoa(int(r.step.GetNumber()))
	}

	return "check-log:" + r.item.checkID
}

func checkLogRowName(row checkLogRow) string {
	if row.step != nil {
		return row.step.GetName()
	}

	return row.item.name
}

// checkLogReadMsg is one chunk of a check's log come back.
type checkLogReadMsg struct {
	generation int
	checkID    string
	resp       *pullrequestv1.ReadCheckLogResponse
	err        error
}

func (c *checkLogsScreen) enter(m *model, target screenTarget) tea.Cmd {
	generation := c.generation + 1
	if target.agentID != "" {
		*c = checkLogsScreen{
			agentID: target.agentID,
			from:    target.from,
		}
	}
	c.generation, c.logs = generation, map[string]*checkLogRead{}

	return tea.Batch(m.readStatusChecksCmd(), c.readsCmd(m))
}

func (c *checkLogsScreen) leaveScreen(*model) tea.Cmd {
	c.generation++

	return nil
}

func (c *checkLogsScreen) update(m *model, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.list().update(m, key, c.listActions())
}

func (c *checkLogsScreen) list() screenList[checkLogRow] {
	return screenList[checkLogRow]{
		cursor: func() *rowCursor { return &c.rowCursor },
		rows:   c.rows,
		name:   checkLogRowName,
		moved: func(m *model) tea.Cmd {
			c.anchor(m)

			return nil
		},
		quit: func(k config.Keybindings) []string { return k.CheckLogs.Quit },
		back: func(k config.Keybindings) []string { return k.CheckLogs.Back },
	}
}

// shown is the checks the status checks screen lists under its scope.
func (c *checkLogsScreen) shown(m *model) []statusCheckItem {
	return xslice.Filter(m.statusChecks.commit.items, m.statusChecks.shownScope().holds)
}

func (c *checkLogsScreen) rows(m *model) []checkLogRow {
	var rows []checkLogRow
	for _, item := range c.shown(m) {
		rows = append(rows, checkLogRow{item: item})
		rows = append(rows, xslice.Map(item.log.GetSteps(), func(step *pullrequestv1.CheckLogStep) checkLogRow {
			return checkLogRow{item: item, step: step}
		})...)
	}

	return narrowed(m.filter, narrowedParams[checkLogRow]{
		rows: rows,
		depth: func(r checkLogRow) int {
			if r.step != nil {
				return 1
			}

			return 0
		},
		name: checkLogRowName,
	})
}

// anchor puts the hovered row's header at the top of the document.
func (c *checkLogsScreen) anchor(m *model) {
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
		m.reading.Line = line
		m.reading.Top = reader.Window{Len: len(rows), Markers: true}.Clamp(line, height)
	}
}

func (c *checkLogsScreen) view(m *model) string {
	return m.columnListView(c.listParams(m))
}

func (c *checkLogsScreen) listParams(m *model) screenListParams {
	return screenListParams{
		tally:    m.statusChecks.commit.tally(),
		help:     c.help,
		empty:    m.statusChecks.emptyLine(m),
		rows:     len(c.rows(m)),
		snapshot: func(pane listPane) view.Snapshot { return c.snapshot(m, pane) },
	}
}

func (c *checkLogsScreen) snapshot(m *model, pane listPane) view.Snapshot {
	rows := c.rows(m)

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r checkLogRow) view.Row {
			name := checkLogRowName(r)
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
				row.Cells = []view.Cell{checkLogStepIcon(r.step)}
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

// checkLogStepIcon is the icon of a step of a log, blank for a step that reported no conclusion.
func checkLogStepIcon(step *pullrequestv1.CheckLogStep) view.Cell {
	if step.GetConclusion() == "" {
		return view.Cell{}
	}

	return statusCheckIcon(statusCheckItem{
		state:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
		conclusion: step.GetConclusion(),
	}, "")
}

func (c *checkLogsScreen) kinds() stops {
	if c.checks == nil {
		return stops{}
	}

	return c.checks.kinds()
}

func (c *checkLogsScreen) kindsFocused() *bool {
	if c.checks == nil {
		return new(bool)
	}

	return c.checks.kindsFocused()
}

func (c *checkLogsScreen) kindsFit(m *model) bool {
	return m.listFitsTabs(c.listParams(m))
}

func (c *checkLogsScreen) kindsSwitch(*model) bool {
	return true
}

// stepKind moves the status checks screen to another scope, which is the scope the logs are joined under.
func (c *checkLogsScreen) stepKind(m *model, delta int) tea.Cmd {
	hovered := c.list().hovered(m)
	cmd := m.statusChecks.stepKind(m, delta)
	c.list().place(m, hovered)

	return tea.Batch(cmd, c.readsCmd(m))
}

func (c *checkLogsScreen) listActions() screenActionSpec {
	return screenActionSpec{
		table: append(c.list().actions(),
			screenAction{
				id:   actionCheckLogsBack,
				keys: func(k config.Keybindings) []string { return k.CheckLogs.Back },
				nav: func(m *model, _ string) tea.Cmd {
					return m.enter(screenStatusChecks, screenTarget{})
				},
			},
		),
		legend: slices.Concat(screenNavLegend(), []screenLegendRow{
			{
				of:    []actionID{actionCheckLogsBack},
				desc:  func(*model) string { return "back" },
				strip: true,
			},
		}),
	}
}

func (c *checkLogsScreen) help(m *model) keyMap {
	return c.listActions().help(m)
}

func (c *checkLogsScreen) parent() screen {
	return c.from
}

func (c *checkLogsScreen) crumbs(m *model) crumbs {
	name := c.agentID
	if item, ok := m.listing.anyAgent(c.agentID); ok {
		name = m.listing.agentName(item.Agent)
	}

	return crumbs{trail: []string{name, "status checks", "logs"}}
}

func (c *checkLogsScreen) doesDrawFromListing() bool {
	return true
}

func (c *checkLogsScreen) takesEveryKey() bool {
	return false
}

func (c *checkLogsScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		read:  isAlwaysRead,
		docRows: func(m *model, width int) []string {
			rows, _ := m.checkLogs.docAnchors(m, width)

			return rows
		},
	}
}

func (c *checkLogsScreen) stageResting(*model) tea.Cmd {
	return nil
}

func (c *checkLogsScreen) dispatches(*model) []dispatching {
	return []dispatching{c.listActions()}
}

var (
	_ kindSwitcher    = (*checkLogsScreen)(nil)
	_ listFilterer    = (*checkLogsScreen)(nil)
	_ crumbDrawer     = (*checkLogsScreen)(nil)
	_ loadingReporter = (*checkLogsScreen)(nil)
	_ screenLeaver    = (*checkLogsScreen)(nil)
	_ docNamer        = (*checkLogsScreen)(nil)
)

func (c *checkLogsScreen) hoveredKey(m *model) string {
	return c.list().hovered(m)
}

func (c *checkLogsScreen) placeAt(m *model, key string) {
	c.list().place(m, key)
}

func (c *checkLogsScreen) filterRows(m *model) []filterRow {
	return c.list().filterRows(m)
}

// docName names the one document every row of the list reads, so moving the cursor keeps the reader's place.
func (c *checkLogsScreen) docName(*model) string {
	return c.agentID + "/check-logs"
}

func (c *checkLogsScreen) isLoading(m *model) bool {
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
func (c *checkLogsScreen) docAnchors(m *model, width int) ([]string, map[string]int) {
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

		anchors[checkLogRow{item: item}.key()] = len(rows)
		title := "═ " + item.name + " "
		rows = append(rows, fitRow(indent+lipgloss.NewStyle().Bold(true).Render(title+strings.Repeat("═", max(width-2*docMargin()-lipgloss.Width(title), 0))), width))

		if notice := c.logNotice(item); notice != "" {
			line(notice, dimStyle())

			continue
		}

		steps := map[int]*pullrequestv1.CheckLogStep{}
		for _, step := range item.log.GetSteps() {
			steps[int(step.GetFirstLine())] = step
		}

		for n, text := range c.logs[item.checkID].lines {
			if step, ok := steps[n+1]; ok {
				anchors[checkLogRow{item: item, step: step}.key()] = len(rows)
				line("▸ "+step.GetName(), lipgloss.NewStyle().Bold(true))
			}

			style := lipgloss.NewStyle()
			if strings.HasPrefix(text, checkLogError) {
				style = paint.Panel.Style(paint.ToneDanger)
			}
			line(strings.ReplaceAll(text, "\t", "    "), style)
		}
	}

	c.drawn = checkLogsDrawn{shape: shape, rows: rows, anchors: anchors}

	return rows, anchors
}

// docShape names everything the document is drawn from, so it is drawn again only once that changes.
func (c *checkLogsScreen) docShape(shown []statusCheckItem, width int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d/%d", c.generation, width)
	for _, item := range shown {
		read := c.logs[item.checkID]
		lines, reading, failed := 0, false, false
		if read != nil {
			lines, reading, failed = len(read.lines), read.reading, read.err != nil
		}
		fmt.Fprintf(&b, "/%s:%d:%d:%t:%t", item.checkID, item.log.GetState(), lines, reading, failed)
	}

	return b.String()
}

// logNotice is what is drawn under a check in place of its log, empty while there are lines to draw.
func (c *checkLogsScreen) logNotice(item statusCheckItem) string {
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
func (c *checkLogsScreen) readsCmd(m *model) tea.Cmd {
	if m.screen != screenCheckLogs || c.logs == nil {
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

		c.logs[item.checkID] = &checkLogRead{reading: true}
		cmds = append(cmds, c.readCmd(m, item.checkID, 1, item.log.GetLineCount()))
	}

	return tea.Batch(cmds...)
}

// readCmd reads one chunk of a check's log, from its first line toward its last.
func (c *checkLogsScreen) readCmd(m *model, checkID string, first, last int32) tea.Cmd {
	generation, clients := c.generation, m.api
	count := min(last-first+1, checkLogChunk)

	return func() tea.Msg {
		resp, err := clients.readCheckLog(checkID, first, count)

		return checkLogReadMsg{generation: generation, checkID: checkID, resp: resp, err: err}
	}
}

func (m *model) applyCheckLogRead(msg checkLogReadMsg) tea.Cmd {
	c := &m.checkLogs
	read, ok := c.logs[msg.checkID]
	if msg.generation != c.generation || !ok {
		return nil
	}

	if msg.err != nil {
		read.err, read.reading = msg.err, false

		return nil
	}

	read.lines = append(read.lines, xslice.Map(msg.resp.GetLines(), func(l *pullrequestv1.CheckLogLine) string { return l.GetText() })...)
	total := msg.resp.GetLineCount()
	if int32(len(read.lines)) >= total || len(msg.resp.GetLines()) == 0 {
		read.reading = false

		return nil
	}

	return c.readCmd(m, msg.checkID, int32(len(read.lines))+1, total)
}
