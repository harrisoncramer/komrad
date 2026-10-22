package ui

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
	// logsReadLimit is the most lines one read of an agent's logs asks for.
	logsReadLimit = 500
	// logsCeiling is the most lines the screen holds before dropping the oldest.
	logsCeiling = 5000
	// logsPollInterval is how long the screen waits after a read before asking again.
	logsPollInterval = time.Second
	logsFilterSettle = 300 * time.Millisecond
)

// logsDeps is what the logs screen is given at launch.
type logsDeps struct {
	keys     config.Keybindings
	glyphs   glyph.Set
	clock    screen.Clock
	register *vim.Register
	clients  api.Clients
	top      *keys.Top
}

// logsScreen lists where one agent's logs, or every run on one task, can be read from and renders
// the newest lines of the source under the cursor beside the list.
type logsScreen struct {
	deps logsDeps
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
	logFilter filterState
	pattern   string
	edits     int
	replacing bool
	// format is how a server log is rendered, and formatFocus says its strip holds the keys.
	format      logview.Format
	formatFocus bool
}

// logSourceRow is one line of the list of log sources.
type logSourceRow struct {
	source *logv1.LogSource
	name   string
	failed bool
}

func (r logSourceRow) Key() string {
	return sourceKey(r.source)
}

// sourceKey tells apart sources of the same name two agents on a task recorded.
func sourceKey(source *logv1.LogSource) string {
	return "log:" + source.GetAgentId() + "/" + source.GetId()
}

// isServerLog reports whether the row is komrad's own structured server log rather than a run's output.
func (r logSourceRow) isServerLog() bool {
	return r.source.Run == nil
}

// logsSourcesMsg is one listing of an agent's log sources come back.
type logsSourcesMsg struct {
	generation int
	sources    []*logv1.LogSource
	err        error
}

// logsReadMsg is one read of an agent's logs come back.
type logsReadMsg struct {
	generation int
	resp       *logv1.ReadAgentLogsResponse
	err        error
}

// logsTickMsg is the wait between two reads run out.
type logsTickMsg struct {
	generation int
}

type logsSettledMsg struct {
	generation int
	edits      int
}

// enter starts over on the agent or task the target names, listing where its logs can be read
// from and claiming a task's failed runs.
func (l *logsScreen) enter(m *model, in screen.Inputs, target screen.Target) screen.Request {
	*l = logsScreen{
		deps:       l.deps,
		agentID:    target.AgentID,
		taskID:     target.TaskID,
		openedFrom: target.OpenedFrom,
		viewer:     logview.NewTail(logsCeiling),
		format:     logview.FormatCompact,
		generation: l.generation + 1,
	}
	if l.taskID == "" {
		return screen.Cmd(l.listCmd())
	}

	taskID := l.taskID

	return screen.Batch{screen.Cmd(l.listCmd()), l.deps.clients.Write("marking the task's runs read", "Could not mark the task's runs read", func(a api.Clients) error {
		return a.MarkTaskRunsRead(taskID)
	})}
}

// leaveScreen ends the chain of reads.
func (l *logsScreen) leaveScreen(*model) screen.Request {
	l.generation++

	return nil
}

func (l *logsScreen) listCmd() tea.Cmd {
	if l.agentID == "" && l.taskID == "" {
		return nil
	}

	agentID, taskID, generation, clients := l.agentID, l.taskID, l.generation, l.deps.clients

	return func() tea.Msg {
		list := clients.ListAgentLogSources
		id := agentID
		if taskID != "" {
			list, id = clients.ListTaskLogSources, taskID
		}
		sources, err := list(id)

		return logsSourcesMsg{
			generation: generation,
			sources:    sources,
			err:        err,
		}
	}
}

func (l *logsScreen) readCmd() tea.Cmd {
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
		Limit:    logsReadLimit,
		Source:   l.reading.GetId(),
		Pattern:  l.pattern,
	}
	generation, clients := l.generation, l.deps.clients

	return func() tea.Msg {
		resp, err := clients.ReadAgentLogs(req)

		return logsReadMsg{
			generation: generation,
			resp:       resp,
			err:        err,
		}
	}
}

// applySources takes a listing of the sources, landing the first on the agent's failed run
// and keeping the cursor on the source it was on after that.
func (l *logsScreen) applySources(m *model, in screen.Inputs, msg logsSourcesMsg) screen.Request {
	if msg.generation != l.generation {
		return nil
	}

	if msg.err != nil {
		l.err = msg.err
		if l.listed {
			return nil
		}

		return screen.Cmd(l.tickCmd(l.deps.clock))
	}

	hovered := list.KeyAt(l.rows(m, in), l.Row)
	l.sources = msg.sources
	if l.listed {
		rows := l.rows(m, in)
		l.Row = cursor.Place(list.IndexOf(rows, hovered), l.Row, len(rows))

		return l.switchSource(m, in)
	}

	l.listed, l.err = true, nil
	l.Row = max(slices.IndexFunc(l.rows(m, in), func(row logSourceRow) bool { return row.failed }), 0)

	return l.switchSource(m, in)
}

func (l *logsScreen) applyRead(msg logsReadMsg) screen.Request {
	if msg.generation != l.generation {
		return nil
	}

	l.err = msg.err
	if msg.err != nil {
		return screen.Cmd(l.tickCmd(l.deps.clock))
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

	return screen.Cmd(l.tickCmd(l.deps.clock))
}

func (l *logsScreen) applyTick(msg logsTickMsg) screen.Request {
	if msg.generation != l.generation {
		return nil
	}

	if !l.listed {
		return screen.Cmd(l.listCmd())
	}

	return screen.Cmd(tea.Batch(l.listCmd(), l.readCmd()))
}

func (l *logsScreen) tickCmd(clock screen.Clock) tea.Cmd {
	generation := l.generation

	return clock.After(logsPollInterval, func(time.Time) tea.Msg { return logsTickMsg{generation: generation} })
}

// switchSource starts the block over on the source the cursor is on, unless it is the one being read.
func (l *logsScreen) switchSource(m *model, in screen.Inputs) screen.Request {
	row, ok := list.At(l.rows(m, in), l.Row)
	if !ok || row.Key() == sourceKey(l.reading) {
		return nil
	}

	l.reading, l.logFocus = row.source, false
	l.logFilter, l.pattern = filterState{}, ""
	l.applyFormat(row)

	return screen.Batch{screen.Cmd(l.restart()), l.markFailedRunRead(row)}
}

// markFailedRunRead claims the agent's failed runs once the run that failed is opened, leaving a
// task's to the screen opening.
func (l *logsScreen) markFailedRunRead(row logSourceRow) screen.Request {
	if !row.failed || l.taskID != "" {
		return nil
	}

	agentID := l.agentID

	return l.deps.clients.Write("marking the failed run read", "Could not mark the failed run read", func(a api.Clients) error { return a.MarkRunsRead(agentID) })
}

// restart drops the lines held and reads the source over again with the pattern the block is read with.
func (l *logsScreen) restart() tea.Cmd {
	l.generation++
	l.viewer.Reset()
	l.readCursor, l.read, l.err, l.replacing = "", false, nil, false

	return l.readCmd()
}

func (l *logsScreen) reread() tea.Cmd {
	l.generation++
	l.readCursor, l.err, l.replacing = "", nil, true

	return l.readCmd()
}

// openLogFilter hands the keys to the prompt at the foot of the log.
func (l *logsScreen) openLogFilter() {
	l.logFilter.typing = true
	l.logFilter.input.Insert()
	l.logFilter.mode = filterModeFilter
}

// updateLogFilter is the prompt at the foot of the log taking keys, reading the log over once the
// pattern sits still or the prompt is left.
func (l *logsScreen) updateLogFilter(key tea.KeyPressMsg) screen.Request {
	if key.String() == "ctrl+c" {
		return screen.Hide{}
	}

	if !l.logFilter.takeKey(key, l.deps.register) {
		return nil
	}
	l.logFilter.mode = filterModeFilter
	if !l.logFilter.typing {
		return l.narrow()
	}

	l.edits++
	generation, edits := l.generation, l.edits

	return screen.Cmd(l.deps.clock.After(logsFilterSettle, func(time.Time) tea.Msg { return logsSettledMsg{generation: generation, edits: edits} }))
}

func (l *logsScreen) applySettled(msg logsSettledMsg) screen.Request {
	if msg.generation != l.generation || msg.edits != l.edits {
		return nil
	}

	return l.narrow()
}

// narrow reads the log over with the pattern typed, unless it is broken or already the one read with.
func (l *logsScreen) narrow() screen.Request {
	if l.logFilter.broken {
		return nil
	}

	pattern := ""
	if l.logFilter.matcher != nil {
		pattern = l.logFilter.matcher.String()
	}
	if pattern == l.pattern {
		return nil
	}

	l.pattern = pattern

	return screen.Cmd(l.reread())
}

// clearLogFilter drops the pattern standing over the log and reads it whole again.
func (l *logsScreen) clearLogFilter() screen.Request {
	l.logFilter.abandon()
	l.logFilter.mode = filterModeFilter

	return l.narrow()
}

func (l *logsScreen) update(m *model, in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || l.viewer == nil {
		return nil
	}

	if l.logFilter.typing {
		return l.updateLogFilter(key)
	}

	if l.isBlockFocused(m, in) {
		return l.actions(m).DispatchTop(l.deps.top, m, l.deps.keys, key.String())
	}

	return l.list().update(m, key, l.actions(m))
}

func (l *logsScreen) receive(m *model, in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case logsSourcesMsg:
		return l.applySources(m, in, msg), true
	case logsReadMsg:
		return l.applyRead(msg), true
	case logsTickMsg:
		return l.applyTick(msg), true
	case logsSettledMsg:
		return l.applySettled(msg), true
	}

	return nil, false
}

func (l *logsScreen) list() screenList[logSourceRow] {
	return screenList[logSourceRow]{
		keys:   l.deps.keys,
		top:    l.deps.top,
		cursor: func() *list.Cursor { return &l.Cursor },
		rows:   l.rows,
		name:   logSourceRowName,
		moved:  func(*model, screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (l *logsScreen) rows(m *model, in screen.Inputs) []logSourceRow {
	var failedRun *int32
	if item, ok := in.Listing.AnyAgent(l.agentID); ok {
		failedRun = item.FailedRunLog
	}

	return list.Narrowed(in.Room.Pattern, list.NarrowedParams[logSourceRow]{
		Rows: xslice.Map(l.sources, func(source *logv1.LogSource) logSourceRow {
			return logSourceRow{
				source: source,
				name:   l.nameSource(in, source),
				failed: source.GetFailed() || (source.Run != nil && failedRun != nil && *source.Run == *failedRun),
			}
		}),
		Depth: func(logSourceRow) int { return 0 },
		Name:  logSourceRowName,
	})
}

func logSourceRowName(row logSourceRow) string {
	return row.name
}

// nameSource is what a source is called, led by the agent that ran it when the screen lists a task's runs.
func (l *logsScreen) nameSource(in screen.Inputs, source *logv1.LogSource) string {
	if l.taskID == "" {
		return sourceLabel(source)
	}

	name := source.GetAgentId()
	if item, ok := in.Listing.AnyAgent(name); ok {
		name = in.Listing.AgentName(item.Agent)
	}

	return name + " " + sourceLabel(source)
}

func (l *logsScreen) listParams(m *model, in screen.Inputs) list.Params {
	rows := l.rows(m, in)
	row, ok := list.At(rows, l.Row)

	return list.Params{
		Help:       l.help(m),
		Empty:      l.emptyLine(),
		Rows:       len(rows),
		Snapshot:   func(pane list.Pane) view.Snapshot { return l.snapshot(m, in, pane) },
		HidesKinds: !ok || !row.isServerLog(),
	}
}

// buildFrame fits the log's cursor and view to the block it is rendered in.
func (l *logsScreen) buildFrame(_ *model, in screen.Inputs) {
	split := in.Room.Split
	if !split.On || !l.read || l.viewer.Len() == 0 {
		return
	}

	l.viewer.Fit(split.Column-len(list.Blank), max(split.Rows-1, 1))
}

func (l *logsScreen) parent() screen.ID {
	return l.openedFrom
}

func (l *logsScreen) crumbs(_ *model, in screen.Inputs) screen.Crumbs {
	if task, ok := in.Listing.Tasks[l.taskID]; ok {
		return screen.Crumbs{Trail: []string{task.Slug, "logs"}}
	}

	name := l.agentID
	if item, ok := in.Listing.AnyAgent(l.agentID); ok {
		name = in.Listing.AgentName(item.Agent)
	}

	return screen.Crumbs{Trail: []string{name, "logs"}}
}

func (l *logsScreen) doesRenderFromListing() bool {
	return true
}

func (l *logsScreen) takesEveryKey() bool {
	return l.isSearchTyping()
}

func (l *logsScreen) isSearchTyping() bool {
	return l.logFilter.typing
}

func (l *logsScreen) column(m *model, in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: columnFloor,
		DocRows: func(width int) []string {
			return m.logs.block(m, in, width)
		},
	}
}

// isBlockRendered reports whether the block beside the list holds lines of the log.
func (l *logsScreen) isBlockRendered(m *model, in screen.Inputs) bool {
	if !in.Room.Split.On {
		return false
	}

	return l.read && l.viewer.Len() > 0
}

// isBlockFocused reports whether the log holds the keys.
func (l *logsScreen) isBlockFocused(m *model, in screen.Inputs) bool {
	if !l.logFocus || !in.Room.ListFocused {
		return false
	}

	if in.Room.Split.On && l.logFilter.isStanding() {
		return true
	}

	return l.isBlockRendered(m, in)
}

func (l *logsScreen) focusBlock(*model) {
	l.logFocus = true
}

func (l *logsScreen) blurBlock() {
	l.logFocus = false
}

func (l *logsScreen) doesScrollColumn(m *model, in screen.Inputs) bool {
	return l.isBlockRendered(m, in)
}

func (l *logsScreen) scrollColumn(_ *model, rows int) screen.Request {
	l.viewer.Scroll(rows)

	return nil
}

func (l *logsScreen) clickLine(key string) {
	line, err := strconv.Atoi(key)
	if err != nil {
		return
	}

	l.viewer.MoveTo(line)
}

// move moves the log's cursor a number of lines toward the newest, or toward the oldest for a negative count.
func (l *logsScreen) move(lines int) screen.Request {
	l.viewer.Move(lines)

	return nil
}

var (
	_ activeLister  = (*logsScreen)(nil)
	_ columnFocuser = (*logsScreen)(nil)
	_ crumbRenderer = (*logsScreen)(nil)
	_ screenLeaver  = (*logsScreen)(nil)
	_ lineTyper     = (*logsScreen)(nil)
	_ kindSwitcher  = (*logsScreen)(nil)
)

func (l *logsScreen) activeList() keyedList {
	return l.list()
}

func (l *logsScreen) typedLine() (field.Line, bool) {
	return l.logFilter.input, l.logFilter.typing
}

func (l *logsScreen) dispatches(*model) []keys.Table {
	return []keys.Table{l.listActions(), l.logActions()}
}

func (l *logsScreen) moveBy(_ *model, rows int) screen.Request {
	return l.move(rows)
}

func (l *logsScreen) moveToEnd(_ *model, direction int) screen.Request {
	return l.move(direction * l.viewer.Len())
}

// logFormatLabels name each format a server log is rendered in, indexed by logview.Format.
var logFormatLabels = []string{"raw", "compact"}

func (l *logsScreen) kinds(*model) tabs.Stops {
	return tabs.Stops{
		Labels: logFormatLabels,
		Focus:  tabs.NoStop,
		Lit:    int(l.format),
	}
}

func (l *logsScreen) kindsFocused(*model) *bool {
	return &l.formatFocus
}

func (l *logsScreen) kindsSwitch(*model) bool {
	return true
}

// stepKind renders the server log in another format.
func (l *logsScreen) stepKind(m *model, in screen.Inputs, delta int) screen.Request {
	l.format = logview.Format(tabs.Step(int(l.format), delta, len(logFormatLabels)))
	if row, ok := list.At(l.rows(m, in), l.Row); ok {
		l.applyFormat(row)
	}

	return nil
}

// applyFormat renders a server log in the chosen format and a run's output as it was written.
func (l *logsScreen) applyFormat(row logSourceRow) {
	if !row.isServerLog() {
		l.viewer.SetFormat(logview.FormatRaw)

		return
	}

	l.viewer.SetFormat(l.format)
}
