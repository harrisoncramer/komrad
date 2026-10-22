package ui

import (
	"slices"
	"strconv"
	"time"

	"komrad/internal/ui/column"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/reader"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
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
	deps statusChecksDeps
	list.Cursor
	agentID string
	// openedFrom is the screen the logs were opened from, which going back returns to.
	openedFrom screen.ID
	// generation tells this visit's reads apart from an earlier one's.
	generation int
	logs       map[string]*statusCheckLogRead
	rendered   statusCheckLogsRendered
	// placed is the row of the document the hovered row's header was last put on, and placedTop
	// the row the window was scrolled to with it.
	placed    int
	placedTop int
}

// statusCheckLogRead is the lines of one check's log read so far.
type statusCheckLogRead struct {
	lines   []string
	err     error
	reading bool
}

// statusCheckLogsRendered is the document as last rendered, kept while nothing it was rendered from has changed.
type statusCheckLogsRendered struct {
	shape   string
	rows    []string
	anchors map[string]int
}

// statusCheckLogRow is a check, or one step of its log, in the list beside the document.
type statusCheckLogRow struct {
	item statusCheckItem
	step *pullrequestv1.CheckLogStep
}

// Key names the row, a status check by its id or one step by the status check id and step number, so the cursor and the document anchors find it.
func (r statusCheckLogRow) Key() string {
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

func (c *statusCheckLogsScreen) enter(m *model, in screen.Inputs, target screen.Target) screen.Request {
	generation := c.generation + 1
	if target.AgentID != "" {
		*c = statusCheckLogsScreen{
			deps:       c.deps,
			agentID:    target.AgentID,
			openedFrom: target.OpenedFrom,
		}
	}
	c.generation, c.logs, c.placed, c.placedTop = generation, map[string]*statusCheckLogRead{}, 0, 0

	return screen.Batch{screen.Cmd(m.readStatusChecksCmd()), screen.Cmd(c.readsCmd(m))}
}

func (c *statusCheckLogsScreen) leaveScreen(*model) screen.Request {
	c.generation++

	return nil
}

func (c *statusCheckLogsScreen) update(m *model, in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.list().update(m, key, c.listActions())
}

func (c *statusCheckLogsScreen) receive(m *model, in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	read, ok := msg.(statusCheckLogReadMsg)
	if !ok {
		return nil, false
	}

	return c.applyRead(m, in, read), true
}

func (c *statusCheckLogsScreen) list() screenList[statusCheckLogRow] {
	return screenList[statusCheckLogRow]{
		keys:   c.deps.keys,
		top:    c.deps.top,
		cursor: func() *list.Cursor { return &c.Cursor },
		rows:   c.rows,
		name:   statusCheckLogRowName,
		moved:  c.anchor,
	}
}

// shown is the checks the status checks screen lists under its scope.
func (c *statusCheckLogsScreen) shown(m *model) []statusCheckItem {
	return xslice.Filter(m.statusChecks.commit.items, m.statusChecks.shownScope().holds)
}

func (c *statusCheckLogsScreen) rows(m *model, in screen.Inputs) []statusCheckLogRow {
	var rows []statusCheckLogRow
	for _, item := range c.shown(m) {
		rows = append(rows, statusCheckLogRow{item: item})
		rows = append(rows, xslice.Map(item.log.GetSteps(), func(step *pullrequestv1.CheckLogStep) statusCheckLogRow {
			return statusCheckLogRow{item: item, step: step}
		})...)
	}

	return list.Narrowed(in.Room.Pattern, list.NarrowedParams[statusCheckLogRow]{
		Rows: rows,
		Depth: func(r statusCheckLogRow) int {
			if r.step != nil {
				return 1
			}

			return 0
		},
		Name: statusCheckLogRowName,
	})
}

// anchor puts the hovered row's header at the top of the document.
func (c *statusCheckLogsScreen) anchor(m *model, in screen.Inputs) screen.Request {
	split := in.Room.Split
	if !split.On {
		return nil
	}

	line, ok := c.keepDoc(m, split.Column).anchors[c.list().hovered(m, in)]
	if !ok {
		return nil
	}

	var top int
	if rows, height, ok := m.docLayout(); ok {
		top = reader.Window{Len: len(rows), Markers: true}.Clamp(line, height)
		c.placed, c.placedTop = line, top
	}

	return screen.PlaceDoc{Line: line, Top: top}
}

// reanchor keeps the hovered row's header in view as the document changes above it, unless the reader has moved or scrolled off it.
func (c *statusCheckLogsScreen) reanchor(m *model, in screen.Inputs) screen.Request {
	if !in.Up || m.reading.focused || m.reading.Line != c.placed || m.reading.Top != c.placedTop {
		return nil
	}

	return c.anchor(m, in)
}

func (c *statusCheckLogsScreen) listParams(m *model, in screen.Inputs) list.Params {
	return list.Params{
		Tally:    m.statusChecks.commit.tally(c.deps.clock.Now()),
		Help:     c.help(m),
		Empty:    m.statusChecks.emptyLine(m),
		Rows:     len(c.rows(m, in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return c.snapshot(m, in, pane) },
	}
}

func (c *statusCheckLogsScreen) kinds(m *model) tabs.Stops {
	return m.statusChecks.kinds(m)
}

func (c *statusCheckLogsScreen) kindsFocused(m *model) *bool {
	return m.statusChecks.kindsFocused(m)
}

func (c *statusCheckLogsScreen) kindsSwitch(*model) bool {
	return true
}

// stepKind moves the status checks screen to another scope, which is the scope the logs are joined under.
func (c *statusCheckLogsScreen) stepKind(m *model, in screen.Inputs, delta int) screen.Request {
	hovered := c.list().hovered(m, in)
	stepped := m.statusChecks.stepKind(m, m.inputsFor(&m.statusChecks), delta)
	placed := c.list().place(m, in, hovered)

	return screen.Batch{stepped, placed, c.reanchor(m, in), screen.Cmd(c.readsCmd(m))}
}

func (c *statusCheckLogsScreen) listActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table: append(c.list().actions(),
			keys.Action[*model]{
				ID:   actionStatusCheckLogsBack,
				Keys: keys.FixedBack,
				Nav: func(m *model, _ string) screen.Request {
					return screen.Open{Screen: screen.StatusChecks}
				},
			},
		),
		Legend: slices.Concat(keys.NavLegend[*model](), []keys.LegendRow[*model]{
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: filterDescOf,
			},
			{
				Of:    []keys.ID{keys.FilterClear},
				Desc:  filterClearDescOf,
				Strip: true,
			},
			{
				Of:    []keys.ID{keys.FilterMatch},
				Desc:  func(*model) string { return "next/prev match" },
				Strip: true,
			},
			{
				Of:    []keys.ID{actionStatusCheckLogsBack},
				Desc:  func(*model) string { return "back" },
				Strip: true,
			},
		}),
	}
}

func (c *statusCheckLogsScreen) help(m *model) legend.Help {
	return m.helpOf(c.listActions())
}

func (c *statusCheckLogsScreen) parent() screen.ID {
	return c.openedFrom
}

func (c *statusCheckLogsScreen) crumbs(_ *model, in screen.Inputs) screen.Crumbs {
	name := c.agentID
	if item, ok := in.Listing.AnyAgent(c.agentID); ok {
		name = in.Listing.AgentName(item.Agent)
	}

	return screen.Crumbs{Trail: []string{name, "status checks", "logs"}}
}

func (c *statusCheckLogsScreen) doesRenderFromListing() bool {
	return true
}

func (c *statusCheckLogsScreen) takesEveryKey() bool {
	return false
}

func (c *statusCheckLogsScreen) column(m *model, in screen.Inputs) column.Spec {
	return column.Spec{
		Floor:           columnFloor,
		Read:            column.IsAlwaysRead,
		SearchDebounce:  statusCheckLogSearchDebounce,
		SearchMinLength: statusCheckLogSearchMinLength,
		DocRows: func(width int) []string {
			return m.statusCheckLogs.doc(m, width).rows
		},
	}
}

func (c *statusCheckLogsScreen) stageResting(*model, screen.Inputs) screen.Request {
	return nil
}

func (c *statusCheckLogsScreen) dispatches(*model) []keys.Table {
	return []keys.Table{c.listActions()}
}

var (
	_ kindSwitcher    = (*statusCheckLogsScreen)(nil)
	_ activeLister    = (*statusCheckLogsScreen)(nil)
	_ crumbRenderer   = (*statusCheckLogsScreen)(nil)
	_ loadingReporter = (*statusCheckLogsScreen)(nil)
	_ screenLeaver    = (*statusCheckLogsScreen)(nil)
	_ docNamer        = (*statusCheckLogsScreen)(nil)
)

func (c *statusCheckLogsScreen) activeList() keyedList {
	return c.list()
}

// docName names the one document every row of the list reads, so moving the cursor keeps the reader's place.
func (c *statusCheckLogsScreen) docName(*model) string {
	return c.agentID + "/status-check-logs"
}

func (c *statusCheckLogsScreen) isLoading(m *model, in screen.Inputs) bool {
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

func (c *statusCheckLogsScreen) buildFrame(m *model, in screen.Inputs) {
	if split := in.Room.Split; split.On {
		c.keepDoc(m, split.Column)
	}
}

// keepDoc builds the document at a width and keeps it for view to read.
func (c *statusCheckLogsScreen) keepDoc(m *model, width int) statusCheckLogsRendered {
	c.rendered = c.doc(m, width)

	return c.rendered
}

// readsCmd starts reading the log of each shown check that has one kept and is not read yet.
func (c *statusCheckLogsScreen) readsCmd(m *model) tea.Cmd {
	if m.screen != screen.StatusCheckLogs || c.logs == nil {
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
		cmds = append(cmds, c.readCmd(item.checkID, 1, item.log.GetLineCount()))
	}

	return tea.Batch(cmds...)
}

// readCmd reads one chunk of a check's log, from its first line toward its last.
func (c *statusCheckLogsScreen) readCmd(checkID string, first, last int32) tea.Cmd {
	generation, clients := c.generation, c.deps.clients
	count := min(last-first+1, statusCheckLogChunk)

	return func() tea.Msg {
		resp, err := clients.ReadCheckLog(checkID, first, count)

		return statusCheckLogReadMsg{
			generation: generation,
			checkID:    checkID,
			resp:       resp,
			err:        err,
		}
	}
}

func (c *statusCheckLogsScreen) applyRead(m *model, in screen.Inputs, msg statusCheckLogReadMsg) screen.Request {
	read, ok := c.logs[msg.checkID]
	if msg.generation != c.generation || !ok {
		return nil
	}

	if msg.err != nil {
		read.err, read.reading = msg.err, false

		return c.reanchor(m, in)
	}

	read.lines = append(read.lines, xslice.Map(msg.resp.GetLines(), func(line *pullrequestv1.CheckLogLine) string { return line.GetText() })...)
	total := msg.resp.GetLineCount()
	var next tea.Cmd
	if int32(len(read.lines)) >= total || len(msg.resp.GetLines()) == 0 {
		read.reading = false
	} else {
		next = c.readCmd(msg.checkID, int32(len(read.lines))+1, total)
	}

	return screen.Batch{c.reanchor(m, in), screen.Cmd(next)}
}
