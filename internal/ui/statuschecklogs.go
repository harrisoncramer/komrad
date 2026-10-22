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

// statusCheckLogs is the logs screen's own state, which the status checks screen keeps.
type statusCheckLogs struct {
	list.Cursor
	agentID string
	// openedFrom is the screen the logs were opened from, which going back returns to.
	openedFrom screen.ID
	// generation tells this visit's reads apart from an earlier one's.
	generation int
	reads      map[string]*statusCheckLogRead
	rendered   statusCheckLogsRendered
	// placed is the row of the document the hovered row's header was last put on, and placedTop
	// the row the window was scrolled to with it.
	placed    int
	placedTop int
}

// statusCheckLogsScreen is the logs screen, its own state paired with the checks the status checks screen lists.
type statusCheckLogsScreen struct {
	*statusCheckLogs
	checks *statusChecksScreen
}

// logsScreen is the logs of the checks the screen lists, as the screen that renders them.
func (c *statusChecksScreen) logsScreen() statusCheckLogsScreen {
	return statusCheckLogsScreen{statusCheckLogs: &c.logs, checks: c}
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

func (c statusCheckLogsScreen) Enter(in screen.Inputs, target screen.Target) screen.Request {
	generation := c.generation + 1
	if target.AgentID != "" {
		*c.statusCheckLogs = statusCheckLogs{
			agentID:    target.AgentID,
			openedFrom: target.OpenedFrom,
		}
	}
	c.generation, c.reads, c.placed, c.placedTop = generation, map[string]*statusCheckLogRead{}, 0, 0

	return screen.Batch{screen.Cmd(c.checks.readStatusChecksCmd(in)), screen.Cmd(c.readsCmd(in))}
}

func (c statusCheckLogsScreen) LeaveScreen() screen.Request {
	c.generation++

	return nil
}

func (c statusCheckLogsScreen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.listActions(in).DispatchTop(c.checks.deps.top, c, c.checks.deps.keys, key.String())
}

func (c statusCheckLogsScreen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case statusCheckLogReadMsg:
		return c.applyRead(in, msg), true
	case statusChecksListedMsg:
		return c.applyListed(in, msg), true
	}

	return nil, false
}

// applyListed keeps the hovered row's header in view and reads the logs of the checks just listed.
func (c statusCheckLogsScreen) applyListed(in screen.Inputs, msg statusChecksListedMsg) screen.Request {
	if msg.agentID != c.checks.agentID || msg.err != nil {
		return nil
	}

	requests := screen.Batch{c.reanchor(in), screen.Cmd(c.readsCmd(in))}
	if !msg.headRead {
		return requests
	}

	return append(requests, screen.Cmd(c.checks.readStatusChecksCmd(in)))
}

func (c statusCheckLogsScreen) list() keys.ScreenList[statusCheckLogRow] {
	return keys.ScreenList[statusCheckLogRow]{
		Keys:   c.checks.deps.keys,
		Top:    c.checks.deps.top,
		Cursor: func() *list.Cursor { return &c.Cursor },
		Rows:   c.rows,
		Name:   statusCheckLogRowName,
		Moved:  c.anchor,
	}
}

// shown is the checks the status checks screen lists under its scope.
func (c statusCheckLogsScreen) shown() []statusCheckItem {
	return xslice.Filter(c.checks.commit.items, c.checks.shownScope().holds)
}

func (c statusCheckLogsScreen) rows(in screen.Inputs) []statusCheckLogRow {
	var rows []statusCheckLogRow
	for _, item := range c.shown() {
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
func (c statusCheckLogsScreen) anchor(in screen.Inputs) screen.Request {
	split := in.Room.Split
	if !split.On {
		return nil
	}

	doc := c.keepDoc(split.Column)
	line, ok := doc.anchors[c.list().Hovered(in)]
	if !ok {
		return nil
	}

	var top int
	if height := in.Room.Doc.Window; height > 0 {
		top = reader.Window{Len: len(doc.rows), Markers: true}.Clamp(line, height)
		c.placed, c.placedTop = line, top
	}

	return screen.PlaceDoc{Line: line, Top: top}
}

// reanchor keeps the hovered row's header in view as the document changes above it, unless the reader has moved or scrolled off it.
func (c statusCheckLogsScreen) reanchor(in screen.Inputs) screen.Request {
	doc := in.Room.Doc
	if !in.Up || doc.Held || doc.Line != c.placed || doc.Top != c.placedTop {
		return nil
	}

	return c.anchor(in)
}

func (c statusCheckLogsScreen) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Tally:    c.checks.commit.tally(c.checks.deps.clock.Now()),
		Help:     c.help(in),
		Empty:    c.checks.emptyLine(in),
		Rows:     len(c.rows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return c.snapshot(in, pane) },
	}
}

func (c statusCheckLogsScreen) Kinds() tabs.Stops {
	return c.checks.Kinds()
}

func (c statusCheckLogsScreen) KindsFocused() *bool {
	return c.checks.KindsFocused()
}

func (c statusCheckLogsScreen) KindsSwitch() bool {
	return true
}

// StepKind moves the status checks screen to another scope, which is the scope the logs are joined under.
func (c statusCheckLogsScreen) StepKind(in screen.Inputs, delta int) screen.Request {
	hovered := c.list().Hovered(in)
	stepped := c.checks.StepKind(in, delta)
	placed := c.list().Place(in, hovered)

	return screen.Batch{stepped, placed, c.reanchor(in), screen.Cmd(c.readsCmd(in))}
}

func (c statusCheckLogsScreen) listActions(in screen.Inputs) keys.Spec[statusCheckLogsScreen] {
	return keys.Spec[statusCheckLogsScreen]{
		Table: append(keys.ListActions[statusCheckLogsScreen](c.list(), in),
			keys.Action[statusCheckLogsScreen]{
				ID:   actionStatusCheckLogsBack,
				Keys: keys.FixedBack,
				Nav: func(statusCheckLogsScreen, string) screen.Request {
					return screen.Open{Screen: screen.StatusChecks}
				},
			},
		),
		Legend: slices.Concat(keys.NavLegend[statusCheckLogsScreen](), []keys.LegendRow[statusCheckLogsScreen]{
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: keys.FilterDescOf[statusCheckLogsScreen](in),
			},
			{
				Of:    []keys.ID{keys.FilterClear},
				Desc:  keys.FilterClearDescOf[statusCheckLogsScreen](in),
				Strip: true,
			},
			{
				Of:    []keys.ID{keys.FilterMatch},
				Desc:  func(statusCheckLogsScreen) string { return "next/prev match" },
				Strip: true,
			},
			{
				Of:    []keys.ID{actionStatusCheckLogsBack},
				Desc:  func(statusCheckLogsScreen) string { return "back" },
				Strip: true,
			},
		}),
	}
}

func (c statusCheckLogsScreen) help(in screen.Inputs) legend.Help {
	return c.listActions(in).Help(c, c.checks.deps.keys)
}

func (c statusCheckLogsScreen) Parent() screen.ID {
	return c.openedFrom
}

func (c statusCheckLogsScreen) Crumbs(in screen.Inputs) screen.Crumbs {
	name := c.agentID
	if item, ok := in.Listing.AnyAgent(c.agentID); ok {
		name = in.Listing.AgentName(item.Agent)
	}

	return screen.Crumbs{Trail: []string{name, "status checks", "logs"}}
}

func (c statusCheckLogsScreen) DoesRenderFromListing() bool {
	return true
}

func (c statusCheckLogsScreen) TakesEveryKey() bool {
	return false
}

func (c statusCheckLogsScreen) Column(screen.Inputs) column.Spec {
	return column.Spec{
		Floor:           column.Narrowest,
		Read:            column.IsAlwaysRead,
		SearchDebounce:  statusCheckLogSearchDebounce,
		SearchMinLength: statusCheckLogSearchMinLength,
		DocRows: func(width int) []string {
			return c.doc(width).rows
		},
	}
}

func (c statusCheckLogsScreen) StageResting(screen.Inputs) screen.Request {
	return nil
}

func (c statusCheckLogsScreen) Dispatches() []keys.Table {
	return []keys.Table{c.listActions(screen.Inputs{})}
}

var (
	_ kindSwitcher    = statusCheckLogsScreen{}
	_ activeLister    = statusCheckLogsScreen{}
	_ crumbRenderer   = statusCheckLogsScreen{}
	_ loadingReporter = statusCheckLogsScreen{}
	_ screenLeaver    = statusCheckLogsScreen{}
	_ docNamer        = statusCheckLogsScreen{}
)

func (c statusCheckLogsScreen) ActiveList() keys.KeyedList {
	return c.list()
}

// DocName names the one document every row of the list reads, so moving the cursor keeps the reader's place.
func (c statusCheckLogsScreen) DocName() string {
	return c.agentID + "/status-check-logs"
}

func (c statusCheckLogsScreen) IsLoading(screen.Inputs) bool {
	if c.checks.listing {
		return true
	}

	for _, read := range c.reads {
		if read.reading {
			return true
		}
	}

	return false
}

func (c statusCheckLogsScreen) BuildFrame(in screen.Inputs) {
	if split := in.Room.Split; split.On {
		c.keepDoc(split.Column)
	}
}

// keepDoc builds the document at a width and keeps it for view to read.
func (c statusCheckLogsScreen) keepDoc(width int) statusCheckLogsRendered {
	c.rendered = c.doc(width)

	return c.rendered
}

// readsCmd starts reading the log of each shown check that has one kept and is not read yet.
func (c statusCheckLogsScreen) readsCmd(in screen.Inputs) tea.Cmd {
	if !in.Up || c.reads == nil {
		return nil
	}

	var cmds []tea.Cmd
	for _, item := range c.shown() {
		if item.log.GetState() != pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED || item.log.GetLineCount() == 0 {
			continue
		}
		if _, ok := c.reads[item.checkID]; ok {
			continue
		}

		c.reads[item.checkID] = &statusCheckLogRead{reading: true}
		cmds = append(cmds, c.readCmd(item.checkID, 1, item.log.GetLineCount()))
	}

	return tea.Batch(cmds...)
}

// readCmd reads one chunk of a check's log, from its first line toward its last.
func (c statusCheckLogsScreen) readCmd(checkID string, first, last int32) tea.Cmd {
	generation, clients := c.generation, c.checks.deps.clients
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

func (c statusCheckLogsScreen) applyRead(in screen.Inputs, msg statusCheckLogReadMsg) screen.Request {
	read, ok := c.reads[msg.checkID]
	if msg.generation != c.generation || !ok {
		return nil
	}

	if msg.err != nil {
		read.err, read.reading = msg.err, false

		return c.reanchor(in)
	}

	read.lines = append(read.lines, xslice.Map(msg.resp.GetLines(), func(line *pullrequestv1.CheckLogLine) string { return line.GetText() })...)
	total := msg.resp.GetLineCount()
	var next tea.Cmd
	if int32(len(read.lines)) >= total || len(msg.resp.GetLines()) == 0 {
		read.reading = false
	} else {
		next = c.readCmd(msg.checkID, int32(len(read.lines))+1, total)
	}

	return screen.Batch{c.reanchor(in), screen.Cmd(next)}
}
