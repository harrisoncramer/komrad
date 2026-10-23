package checks

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

// logChunk is the most lines one read of a check's log asks for.
const logChunk = 5000

// logErrorMarker is the prefix GitHub Actions puts at the start of a log line the job reported as an error.
const logErrorMarker = "##[error]"

const (
	logSearchDebounce  = 150 * time.Millisecond
	logSearchMinLength = 3
)

// logs is the logs screen's own state, which the status checks screen keeps.
type logs struct {
	list.Cursor
	agentID string
	// openedFrom is the screen the logs were opened from, which going back returns to.
	openedFrom screen.ID
	// generation tells this visit's reads apart from an earlier one's.
	generation int
	reads      map[string]*logRead
	rendered   logsRendered
	// placed is the row of the document the hovered row's header was last put on, and placedTop
	// the row the window was scrolled to with it.
	placed    int
	placedTop int
}

// Logs is the logs screen, its own state paired with the checks the status checks screen lists.
type Logs struct {
	*logs
	checks *Screen
}

// Logs is the logs of the checks the screen lists, as the screen that renders them.
func (c *Screen) Logs() Logs {
	return Logs{logs: &c.logs, checks: c}
}

// logRead is the lines of one check's log read so far.
type logRead struct {
	lines   []string
	err     error
	reading bool
}

// logsRendered is the document as last rendered, kept while nothing it was rendered from has changed.
type logsRendered struct {
	shape   string
	rows    []string
	anchors map[string]int
}

// logRow is a check, or one step of its log, in the list beside the document.
type logRow struct {
	item check
	step *pullrequestv1.CheckLogStep
}

// Key names the row, a status check by its id or one step by the status check id and step number, so the cursor and the document anchors find it.
func (r logRow) Key() string {
	if r.step != nil {
		return "status-check-log:" + r.item.checkID + "/" + strconv.Itoa(int(r.step.GetNumber()))
	}

	return "status-check-log:" + r.item.checkID
}

func logRowName(row logRow) string {
	if row.step != nil {
		return row.step.GetName()
	}

	return row.item.name
}

// logReadMsg is one chunk of a check's log come back.
type logReadMsg struct {
	generation int
	checkID    string
	resp       *pullrequestv1.ReadCheckLogResponse
	err        error
}

// Enter starts a new visit, listing the status checks again and reading their logs.
func (c Logs) Enter(in screen.Inputs, target screen.Target) screen.Request {
	generation := c.generation + 1
	if target.AgentID != "" {
		*c.logs = logs{
			agentID:    target.AgentID,
			openedFrom: target.OpenedFrom,
		}
	}
	c.generation, c.reads, c.placed, c.placedTop = generation, map[string]*logRead{}, 0, 0

	return screen.Batch{screen.Cmd(c.checks.listCmd(in)), screen.Cmd(c.readsCmd(in))}
}

// LeaveScreen ends the visit's reads.
func (c Logs) LeaveScreen() screen.Request {
	c.generation++

	return nil
}

// Update sends a key to the screen's table.
func (c Logs) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.listActions(in).DispatchTop(c.checks.deps.Top, c, c.checks.deps.Keys, key.String())
}

// Receive takes the chunks of log read, the status checks as they are listed, and the listing moving.
func (c Logs) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case logReadMsg:
		return c.applyRead(in, msg), true
	case listedMsg:
		return c.applyListed(in, msg), true
	case screen.Relisted:
		return screen.Cmd(c.checks.listCmd(in)), true
	}

	return nil, false
}

// applyListed keeps the hovered row's header in view and reads the logs of the checks just listed.
func (c Logs) applyListed(in screen.Inputs, msg listedMsg) screen.Request {
	if msg.agentID != c.checks.agentID || msg.err != nil {
		return nil
	}

	requests := screen.Batch{c.reanchor(in), screen.Cmd(c.readsCmd(in))}
	if !msg.headRead {
		return requests
	}

	return append(requests, screen.Cmd(c.checks.listCmd(in)))
}

func (c Logs) list() keys.ScreenList[logRow] {
	return keys.ScreenList[logRow]{
		Keys:   c.checks.deps.Keys,
		Cursor: func() *list.Cursor { return &c.Cursor },
		Rows:   c.rows,
		Name:   logRowName,
		Moved:  c.anchor,
	}
}

// shown is the checks the status checks screen lists under its scope.
func (c Logs) shown() []check {
	return xslice.Filter(c.checks.commit.items, c.checks.shownScope().holds)
}

func (c Logs) rows(in screen.Inputs) []logRow {
	var rows []logRow
	for _, item := range c.shown() {
		rows = append(rows, logRow{item: item})
		rows = append(rows, xslice.Map(item.log.GetSteps(), func(step *pullrequestv1.CheckLogStep) logRow {
			return logRow{item: item, step: step}
		})...)
	}

	return list.Narrowed(in.Room.Pattern, list.NarrowedParams[logRow]{
		Rows: rows,
		Depth: func(r logRow) int {
			if r.step != nil {
				return 1
			}

			return 0
		},
		Name: logRowName,
	})
}

// anchor puts the hovered row's header at the top of the document.
func (c Logs) anchor(in screen.Inputs) screen.Request {
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
func (c Logs) reanchor(in screen.Inputs) screen.Request {
	doc := in.Room.Doc
	if !in.Up || doc.Held || doc.Line != c.placed || doc.Top != c.placedTop {
		return nil
	}

	return c.anchor(in)
}

// ListParams is the list of status checks and their steps as the root renders it.
func (c Logs) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Tally:    c.checks.commit.tally(c.checks.deps.Clock.Now()),
		Help:     c.help(in),
		Empty:    c.checks.emptyLine(in),
		Rows:     len(c.rows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return c.snapshot(in, pane) },
	}
}

// Kinds is the status checks screen's row of scopes.
func (c Logs) Kinds() tabs.Stops {
	return c.checks.Kinds()
}

// KindsFocused is whether the status checks screen's row of scopes holds the keys.
func (c Logs) KindsFocused() *bool {
	return c.checks.KindsFocused()
}

// KindsSwitch reports that the scope may always change.
func (c Logs) KindsSwitch() bool {
	return true
}

// StepKind moves the status checks screen to another scope, which is the scope the logs are joined under.
func (c Logs) StepKind(in screen.Inputs, delta int) screen.Request {
	hovered := c.list().Hovered(in)
	stepped := c.checks.StepKind(in, delta)
	placed := c.list().Place(in, hovered)

	return screen.Batch{stepped, placed, c.reanchor(in), screen.Cmd(c.readsCmd(in))}
}

func (c Logs) listActions(in screen.Inputs) keys.Spec[Logs] {
	return keys.Spec[Logs]{
		Table: append(keys.ListActions[Logs](c.list(), in),
			keys.Action[Logs]{
				ID:   actionLogsBack,
				Keys: keys.FixedBack,
				Nav: func(Logs, string) screen.Request {
					return screen.Open{Screen: screen.StatusChecks}
				},
			},
		),
		Legend: slices.Concat(keys.NavLegend[Logs](), []keys.LegendRow[Logs]{
			{
				ActionIDs: []keys.ID{keys.FilterOpen},
				Desc:      keys.FilterDescOf[Logs](in),
			},
			{
				ActionIDs: []keys.ID{keys.FilterClear},
				Desc:      keys.FilterClearDescOf[Logs](in),
				Strip:     true,
			},
			{
				ActionIDs: []keys.ID{keys.FilterMatch},
				Desc:      func(Logs) string { return "next/prev match" },
				Strip:     true,
			},
			{
				ActionIDs: []keys.ID{actionLogsBack},
				Desc:      func(Logs) string { return "back" },
				Strip:     true,
			},
		}),
	}
}

func (c Logs) help(in screen.Inputs) legend.Help {
	return c.listActions(in).Help(c, c.checks.deps.Keys)
}

// Parent is the screen the logs were opened from.
func (c Logs) Parent() screen.ID {
	return c.openedFrom
}

// Crumbs is the trail down to the logs of the agent's status checks.
func (c Logs) Crumbs(in screen.Inputs) screen.Crumbs {
	name := c.agentID
	if item, ok := in.Listing.AnyAgent(c.agentID); ok {
		name = in.Listing.AgentName(item.Agent)
	}

	return screen.Crumbs{Trail: []string{name, "status checks", "logs"}}
}

// DoesRenderFromListing reports that the screen names the agent from the listing.
func (c Logs) DoesRenderFromListing() bool {
	return true
}

// TakesEveryKey reports that nothing on the screen takes every key.
func (c Logs) TakesEveryKey() bool {
	return false
}

// Column is the logs rendered in the block beside the list.
func (c Logs) Column(screen.Inputs) column.Spec {
	return column.Spec{
		Floor:           column.Narrowest,
		Read:            column.IsAlwaysRead,
		SearchDebounce:  logSearchDebounce,
		SearchMinLength: logSearchMinLength,
		DocRows: func(width int) []string {
			return c.doc(width).rows
		},
	}
}

// StageResting stages nothing for the row the cursor rests on.
func (c Logs) StageResting(screen.Inputs) screen.Request {
	return nil
}

// Dispatches is the screen's one table.
func (c Logs) Dispatches() []keys.Table {
	return []keys.Table{c.listActions(screen.Inputs{})}
}

// ActiveList is the list of status checks and steps the filter narrows.
func (c Logs) ActiveList() keys.KeyedList {
	return c.list()
}

// DocName names the one document every row of the list reads, so moving the cursor keeps the reader's place.
func (c Logs) DocName() string {
	return c.agentID + "/status-check-logs"
}

// IsLoading reports whether the status checks are being listed or any log is being read.
func (c Logs) IsLoading(screen.Inputs) bool {
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

// BuildFrame renders the logs at the width of the block beside the list.
func (c Logs) BuildFrame(in screen.Inputs) {
	if split := in.Room.Split; split.On {
		c.keepDoc(split.Column)
	}
}

// keepDoc builds the document at a width and keeps it for view to read.
func (c Logs) keepDoc(width int) logsRendered {
	c.rendered = c.doc(width)

	return c.rendered
}

// readsCmd starts reading the log of each shown check that has one kept and is not read yet.
func (c Logs) readsCmd(in screen.Inputs) tea.Cmd {
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

		c.reads[item.checkID] = &logRead{reading: true}
		cmds = append(cmds, c.readCmd(item.checkID, 1, item.log.GetLineCount()))
	}

	return tea.Batch(cmds...)
}

// readCmd reads one chunk of a check's log, from its first line toward its last.
func (c Logs) readCmd(checkID string, first, last int32) tea.Cmd {
	generation, clients := c.generation, c.checks.deps.Clients
	count := min(last-first+1, logChunk)

	return func() tea.Msg {
		resp, err := clients.ReadCheckLog(checkID, first, count)

		return logReadMsg{
			generation: generation,
			checkID:    checkID,
			resp:       resp,
			err:        err,
		}
	}
}

func (c Logs) applyRead(in screen.Inputs, msg logReadMsg) screen.Request {
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
