package logs

import (
	"slices"
	"strconv"
	"time"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/api"
	"komrad/internal/ui/column"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/field"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/logview"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"
	"komrad/internal/vim"
	"komrad/lib/xslice"
	logv1 "komrad/proto/gen/log/v1"

	tea "charm.land/bubbletea/v2"
)

const (
	// readLimit is the most lines one read of an agent's logs asks for.
	readLimit = 500
	// ceiling is the most lines the screen holds before dropping the oldest.
	ceiling = 5000
	// pollInterval is how long the screen waits after a read before asking again.
	pollInterval = time.Second
	filterSettle = 300 * time.Millisecond
)

// Deps is what the logs screen is given at launch.
type Deps struct {
	Keys     config.Keybindings
	Glyphs   glyph.Set
	Clock    screen.Clock
	Register *vim.Register
	Clients  api.Clients
	Top      *keys.Top
}

// Screen lists where one agent's logs, or every run on one task, can be read from and renders
// the newest lines of the source under the cursor beside the list.
type Screen struct {
	deps Deps
	list.Cursor
	agentID string
	// taskID is the task whose runs the screen lists, empty for an agent's logs.
	taskID string
	// openedFrom is the view that opened the screen.
	openedFrom screen.ID
	sources    []*logv1.LogSource
	listed     bool
	// reading is the source the block holds lines of.
	reading    *logv1.LogSource
	viewer     *logview.Tail
	readCursor string
	// generation tells this visit's and this source's reads apart from an earlier one's.
	generation int
	read       bool
	err        error
	// logFocus says the picker handed the keys to the log.
	logFocus bool
	// logFilter is the pattern typed at the foot of the log, and pattern is the one the block was read with.
	logFilter filter.State
	pattern   string
	edits     int
	replacing bool
	// format is how a server log is rendered, and formatFocus says its strip holds the keys.
	format      logview.Format
	formatFocus bool
}

// New is the logs screen built from what it is given at launch.
func New(deps Deps) Screen {
	return Screen{deps: deps}
}

// sourceRow is one line of the list of log sources.
type sourceRow struct {
	source *logv1.LogSource
	name   string
	failed bool
}

func (r sourceRow) Key() string {
	return sourceKey(r.source)
}

// sourceKey tells apart sources of the same name two agents on a task recorded.
func sourceKey(source *logv1.LogSource) string {
	return "log:" + source.GetAgentId() + "/" + source.GetId()
}

// isServerLog reports whether the row is komrad's own structured server log rather than a run's output.
func (r sourceRow) isServerLog() bool {
	return r.source.Run == nil
}

// sourcesMsg is one listing of an agent's log sources come back.
type sourcesMsg struct {
	generation int
	sources    []*logv1.LogSource
	err        error
}

// readMsg is one read of an agent's logs come back.
type readMsg struct {
	generation int
	resp       *logv1.ReadAgentLogsResponse
	err        error
}

// tickMsg is the wait between two reads run out.
type tickMsg struct {
	generation int
}

type settledMsg struct {
	generation int
	edits      int
}

// Enter starts over on the agent or task the target names, listing where its logs can be read
// from and claiming a task's failed runs.
func (l *Screen) Enter(in screen.Inputs, target screen.Target) screen.Request {
	*l = Screen{
		deps:       l.deps,
		agentID:    target.AgentID,
		taskID:     target.TaskID,
		openedFrom: target.OpenedFrom,
		viewer:     logview.NewTail(ceiling),
		format:     logview.FormatCompact,
		generation: l.generation + 1,
	}
	if l.taskID == "" {
		return screen.Cmd(l.listCmd())
	}

	taskID := l.taskID

	return screen.Batch{screen.Cmd(l.listCmd()), l.deps.Clients.Write("marking the task's runs read", "Could not mark the task's runs read", func(a api.Clients) error {
		return a.MarkTaskRunsRead(taskID)
	})}
}

// LeaveScreen ends the chain of reads.
func (l *Screen) LeaveScreen() screen.Request {
	l.generation++

	return nil
}

func (l *Screen) listCmd() tea.Cmd {
	if l.agentID == "" && l.taskID == "" {
		return nil
	}

	agentID, taskID, generation, clients := l.agentID, l.taskID, l.generation, l.deps.Clients

	return func() tea.Msg {
		list := clients.ListAgentLogSources
		id := agentID
		if taskID != "" {
			list, id = clients.ListTaskLogSources, taskID
		}
		sources, err := list(id)

		return sourcesMsg{
			generation: generation,
			sources:    sources,
			err:        err,
		}
	}
}

func (l *Screen) readCmd() tea.Cmd {
	if l.reading == nil {
		return nil
	}

	agentID := l.reading.GetAgentId()
	if agentID == "" {
		agentID = l.agentID
	}
	req := &logv1.ReadAgentLogsRequest{
		AgentIds: []string{agentID},
		Cursor:   l.readCursor,
		Limit:    readLimit,
		Source:   l.reading.GetId(),
		Pattern:  l.pattern,
	}
	generation, clients := l.generation, l.deps.Clients

	return func() tea.Msg {
		resp, err := clients.ReadAgentLogs(req)

		return readMsg{
			generation: generation,
			resp:       resp,
			err:        err,
		}
	}
}

// applySources takes a listing of the sources, landing the first on the agent's failed run
// and keeping the cursor on the source it was on after that.
func (l *Screen) applySources(in screen.Inputs, msg sourcesMsg) screen.Request {
	if msg.generation != l.generation {
		return nil
	}

	if msg.err != nil {
		l.err = msg.err
		if l.listed {
			return nil
		}

		return screen.Cmd(l.tickCmd(l.deps.Clock))
	}

	hovered := list.KeyAt(l.rows(in), l.Row)
	l.sources = msg.sources
	if l.listed {
		rows := l.rows(in)
		l.Row = cursor.Place(list.IndexOf(rows, hovered), l.Row, len(rows))

		return l.switchSource(in)
	}

	l.listed, l.err = true, nil
	l.Row = max(slices.IndexFunc(l.rows(in), func(row sourceRow) bool { return row.failed }), 0)

	return l.switchSource(in)
}

func (l *Screen) applyRead(msg readMsg) screen.Request {
	if msg.generation != l.generation {
		return nil
	}

	l.err = msg.err
	if msg.err != nil {
		return screen.Cmd(l.tickCmd(l.deps.Clock))
	}

	if msg.resp.GetReset_() || l.replacing {
		l.viewer.Reset()
	}
	l.replacing = false
	l.viewer.Append(msg.resp.GetLines())
	l.readCursor, l.read = msg.resp.GetCursor(), true

	if msg.resp.GetMore() {
		return screen.Cmd(l.readCmd())
	}

	return screen.Cmd(l.tickCmd(l.deps.Clock))
}

func (l *Screen) applyTick(msg tickMsg) screen.Request {
	if msg.generation != l.generation {
		return nil
	}

	if !l.listed {
		return screen.Cmd(l.listCmd())
	}

	return screen.Cmd(tea.Batch(l.listCmd(), l.readCmd()))
}

func (l *Screen) tickCmd(clock screen.Clock) tea.Cmd {
	generation := l.generation

	return clock.After(pollInterval, func(time.Time) tea.Msg { return tickMsg{generation: generation} })
}

// switchSource starts the block over on the source the cursor is on, unless it is the one being read.
func (l *Screen) switchSource(in screen.Inputs) screen.Request {
	row, ok := list.At(l.rows(in), l.Row)
	if !ok || row.Key() == sourceKey(l.reading) {
		return nil
	}

	l.reading, l.logFocus = row.source, false
	l.logFilter, l.pattern = filter.State{}, ""
	l.applyFormat(row)

	return screen.Batch{screen.Cmd(l.restart()), l.markFailedRunRead(row)}
}

// StageResting reads the source the cursor came to rest on.
func (l *Screen) StageResting(in screen.Inputs) screen.Request {
	return l.switchSource(in)
}

// markFailedRunRead claims the agent's failed runs once the run that failed is opened, leaving a
// task's to the screen opening.
func (l *Screen) markFailedRunRead(row sourceRow) screen.Request {
	if !row.failed || l.taskID != "" {
		return nil
	}

	agentID := l.agentID

	return l.deps.Clients.Write("marking the failed run read", "Could not mark the failed run read", func(a api.Clients) error { return a.MarkRunsRead(agentID) })
}

// restart drops the lines held and reads the source over again with the pattern the block is read with.
func (l *Screen) restart() tea.Cmd {
	l.generation++
	l.viewer.Reset()
	l.readCursor, l.read, l.err, l.replacing = "", false, nil, false

	return l.readCmd()
}

func (l *Screen) reread() tea.Cmd {
	l.generation++
	l.readCursor, l.err, l.replacing = "", nil, true

	return l.readCmd()
}

// openLogFilter hands the keys to the prompt at the foot of the log.
func (l *Screen) openLogFilter() {
	l.logFilter.IsTyping = true
	l.logFilter.Input.Insert()
	l.logFilter.Mode = filter.ModeFilter
}

// updateLogFilter is the prompt at the foot of the log taking keys, reading the log over once the
// pattern sits still or the prompt is left.
func (l *Screen) updateLogFilter(key tea.KeyPressMsg) screen.Request {
	if key.String() == "ctrl+c" {
		return screen.Hide{}
	}

	if !l.logFilter.TakeKey(key, l.deps.Register) {
		return nil
	}
	l.logFilter.Mode = filter.ModeFilter
	if !l.logFilter.IsTyping {
		return l.narrow()
	}

	l.edits++
	generation, edits := l.generation, l.edits

	return screen.Cmd(l.deps.Clock.After(filterSettle, func(time.Time) tea.Msg { return settledMsg{generation: generation, edits: edits} }))
}

func (l *Screen) applySettled(msg settledMsg) screen.Request {
	if msg.generation != l.generation || msg.edits != l.edits {
		return nil
	}

	return l.narrow()
}

// narrow reads the log over with the pattern typed, unless it is broken or already the one read with.
func (l *Screen) narrow() screen.Request {
	if l.logFilter.IsPatternBroken {
		return nil
	}

	pattern := ""
	if l.logFilter.Matcher != nil {
		pattern = l.logFilter.Matcher.String()
	}
	if pattern == l.pattern {
		return nil
	}

	l.pattern = pattern

	return screen.Cmd(l.reread())
}

// clearLogFilter drops the pattern standing over the log and reads it whole again.
func (l *Screen) clearLogFilter() screen.Request {
	l.logFilter.Abandon()
	l.logFilter.Mode = filter.ModeFilter

	return l.narrow()
}

// Update sends a key to the prompt at the foot of the log while it is typed into, and to the screen's table otherwise.
func (l *Screen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || l.viewer == nil {
		return nil
	}

	if l.logFilter.IsTyping {
		return l.updateLogFilter(key)
	}

	return l.actions(in).DispatchTop(l.deps.Top, l, l.deps.Keys, key.String())
}

// Receive takes the screen's own listings, reads, ticks and settled patterns.
func (l *Screen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case sourcesMsg:
		return l.applySources(in, msg), true
	case readMsg:
		return l.applyRead(msg), true
	case tickMsg:
		return l.applyTick(msg), true
	case settledMsg:
		return l.applySettled(msg), true
	}

	return nil, false
}

func (l *Screen) list() keys.ScreenList[sourceRow] {
	return keys.ScreenList[sourceRow]{
		Keys:   l.deps.Keys,
		Cursor: func() *list.Cursor { return &l.Cursor },
		Rows:   l.rows,
		Name:   sourceRowName,
		Moved:  func(screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (l *Screen) rows(in screen.Inputs) []sourceRow {
	var failedRun *int32
	if item, ok := in.Listing.AnyAgent(l.agentID); ok {
		failedRun = item.FailedRunLog
	}

	return list.Narrowed(in.Room.Pattern, list.NarrowedParams[sourceRow]{
		Rows: xslice.Map(l.sources, func(source *logv1.LogSource) sourceRow {
			return sourceRow{
				source: source,
				name:   l.nameSource(in, source),
				failed: source.GetFailed() || (source.Run != nil && failedRun != nil && *source.Run == *failedRun),
			}
		}),
		Depth: func(sourceRow) int { return 0 },
		Name:  sourceRowName,
	})
}

func sourceRowName(row sourceRow) string {
	return row.name
}

// nameSource is what a source is called, led by the agent that ran it when the screen lists a task's runs.
func (l *Screen) nameSource(in screen.Inputs, source *logv1.LogSource) string {
	if l.taskID == "" {
		return sourceLabel(source)
	}

	name := source.GetAgentId()
	if item, ok := in.Listing.AnyAgent(name); ok {
		name = in.Listing.AgentName(item.Agent)
	}

	return name + " " + sourceLabel(source)
}

// ListParams is the list of log sources as the root renders it, with the kinds row hidden unless the server log is hovered.
func (l *Screen) ListParams(in screen.Inputs) list.Params {
	rows := l.rows(in)
	row, ok := list.At(rows, l.Row)

	return list.Params{
		Help:       l.help(in),
		Empty:      l.emptyLine(),
		Rows:       len(rows),
		Snapshot:   func(pane list.Pane) view.Snapshot { return l.snapshot(in, pane) },
		HidesKinds: !ok || !row.isServerLog(),
	}
}

// BuildFrame fits the log's cursor and view to the block it is rendered in.
func (l *Screen) BuildFrame(in screen.Inputs) {
	split := in.Room.OwnBlockSplit
	if !split.IsDivided || !l.read || l.viewer.Len() == 0 {
		return
	}

	l.viewer.Fit(split.ColumnWidth-len(list.Blank), max(split.RowCount-1, 1))
}

// Parent is the screen the logs were opened from.
func (l *Screen) Parent() screen.ID {
	return l.openedFrom
}

// Crumbs is the trail down to the task's or the agent's logs.
func (l *Screen) Crumbs(in screen.Inputs) screen.Crumbs {
	if task, ok := in.Listing.Tasks[l.taskID]; ok {
		return screen.Crumbs{Trail: []string{task.Slug, "logs"}}
	}

	name := l.agentID
	if item, ok := in.Listing.AnyAgent(l.agentID); ok {
		name = in.Listing.AgentName(item.Agent)
	}

	return screen.Crumbs{Trail: []string{name, "logs"}}
}

// DoesRenderFromListing reports that the screen names agents and tasks from the listing.
func (l *Screen) DoesRenderFromListing() bool {
	return true
}

// TakesEveryKey reports whether the prompt at the foot of the log is typed into.
func (l *Screen) TakesEveryKey() bool {
	return l.IsSearchTyping()
}

// IsSearchTyping reports whether the prompt at the foot of the log is typed into.
func (l *Screen) IsSearchTyping() bool {
	return l.logFilter.IsTyping
}

// Column is the log rendered in the block beside the list.
func (l *Screen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		DocRows: func(width int) []string {
			return l.block(in, width)
		},
	}
}

// IsBlockRendered reports whether the block beside the list holds lines of the log.
func (l *Screen) IsBlockRendered(in screen.Inputs) bool {
	if !in.Room.OwnBlockSplit.IsDivided {
		return false
	}

	return l.read && l.viewer.Len() > 0
}

// IsBlockFocused reports whether the log holds the keys.
func (l *Screen) IsBlockFocused(in screen.Inputs) bool {
	if !l.logFocus || !in.Room.IsListFocused {
		return false
	}

	if in.Room.OwnBlockSplit.IsDivided && l.logFilter.IsStanding() {
		return true
	}

	return l.IsBlockRendered(in)
}

// FocusBlock hands the keys to the log.
func (l *Screen) FocusBlock() {
	l.logFocus = true
}

// BlurBlock hands the keys back to the list.
func (l *Screen) BlurBlock() {
	l.logFocus = false
}

// DoesScrollColumn reports whether the log is rendered beside the list to scroll.
func (l *Screen) DoesScrollColumn(in screen.Inputs) bool {
	return l.IsBlockRendered(in)
}

// ScrollColumn scrolls the log by a number of rows.
func (l *Screen) ScrollColumn(in screen.Inputs, rows int) screen.Request {
	l.viewer.Scroll(rows)

	return nil
}

// ClickLine moves the log's cursor to the line a click landed on.
func (l *Screen) ClickLine(key string) {
	line, err := strconv.Atoi(key)
	if err != nil {
		return
	}

	l.viewer.MoveTo(line)
}

// move moves the log's cursor a number of lines toward the newest, or toward the oldest for a negative count.
func (l *Screen) move(lines int) screen.Request {
	l.viewer.Move(lines)

	return nil
}

// ActiveList is the list of log sources the filter narrows.
func (l *Screen) ActiveList() keys.KeyedList {
	return l.list()
}

// TypedLine is the prompt at the foot of the log and whether it is typed into.
func (l *Screen) TypedLine() (field.Line, bool) {
	return l.logFilter.Input, l.logFilter.IsTyping
}

// Dispatches is the list's table and the log's table.
func (l *Screen) Dispatches() []keys.Table {
	return []keys.Table{l.listActions(screen.Inputs{}), l.logActions(screen.Inputs{})}
}

// MoveBy moves the log's cursor a number of lines.
func (l *Screen) MoveBy(_ screen.Inputs, rows int) screen.Request {
	return l.move(rows)
}

// MoveToEnd moves the log's cursor to the newest line, or to the oldest for a negative direction.
func (l *Screen) MoveToEnd(_ screen.Inputs, direction int) screen.Request {
	return l.move(direction * l.viewer.Len())
}

// formatLabels name each format a server log is rendered in, indexed by logview.Format.
var formatLabels = []string{"raw", "compact"}

// Kinds is the row of formats a server log is rendered in.
func (l *Screen) Kinds() tabs.Stops {
	return tabs.Stops{
		Labels:      formatLabels,
		FocusedStop: tabs.NoStop,
		ChosenStop:  int(l.format),
	}
}

// KindsFocused is whether the row of formats holds the keys.
func (l *Screen) KindsFocused() *bool {
	return &l.formatFocus
}

// KindsSwitch reports that the format may always change.
func (l *Screen) KindsSwitch() bool {
	return true
}

// StepKind renders the server log in another format.
func (l *Screen) StepKind(in screen.Inputs, delta int) screen.Request {
	l.format = logview.Format(tabs.StepStop(int(l.format), delta, len(formatLabels)))
	if row, ok := list.At(l.rows(in), l.Row); ok {
		l.applyFormat(row)
	}

	return nil
}

// applyFormat renders a server log in the chosen format and a run's output as it was written.
func (l *Screen) applyFormat(row sourceRow) {
	if !row.isServerLog() {
		l.viewer.SetFormat(logview.FormatRaw)

		return
	}

	l.viewer.SetFormat(l.format)
}
