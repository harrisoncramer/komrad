package ui

import (
	"slices"
	"strconv"
	"time"

	"komrad/internal/ui/cursor"
	"komrad/internal/ui/field"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/logview"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
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

// logsScreen lists where one agent's logs, or every run on one task, can be read from and renders
// the newest lines of the source under the cursor beside the list.
type logsScreen struct {
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
func (l *logsScreen) enter(m *model, target screen.Target) screen.Request {
	*l = logsScreen{
		agentID:    target.AgentID,
		taskID:     target.TaskID,
		openedFrom: target.OpenedFrom,
		viewer:     logview.NewTail(logsCeiling),
		format:     logview.FormatCompact,
		generation: l.generation + 1,
	}
	if l.taskID == "" {
		return screen.Cmd(l.listCmd(m))
	}

	taskID := l.taskID

	return screen.Batch{screen.Cmd(l.listCmd(m)), m.write("marking the task's runs read", "Could not mark the task's runs read", func(a api) error {
		return a.markTaskRunsRead(taskID)
	})}
}

// leaveScreen ends the chain of reads.
func (l *logsScreen) leaveScreen(*model) screen.Request {
	l.generation++

	return nil
}

func (l *logsScreen) listCmd(m *model) tea.Cmd {
	if l.agentID == "" && l.taskID == "" {
		return nil
	}

	agentID, taskID, generation, clients := l.agentID, l.taskID, l.generation, m.api.reads()

	return func() tea.Msg {
		list := clients.listAgentLogSources
		id := agentID
		if taskID != "" {
			list, id = clients.listTaskLogSources, taskID
		}
		sources, err := list(id)

		return logsSourcesMsg{
			generation: generation,
			sources:    sources,
			err:        err,
		}
	}
}

func (l *logsScreen) readCmd(m *model) tea.Cmd {
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
	generation, clients := l.generation, m.api.reads()

	return func() tea.Msg {
		resp, err := clients.readAgentLogs(req)

		return logsReadMsg{
			generation: generation,
			resp:       resp,
			err:        err,
		}
	}
}

// applyLogsSources takes a listing of the sources, landing the first on the agent's failed run
// and keeping the cursor on the source it was on after that.
func (m *model) applyLogsSources(msg logsSourcesMsg) screen.Request {
	l := &m.logs
	if msg.generation != l.generation {
		return nil
	}

	if msg.err != nil {
		l.err = msg.err
		if l.listed {
			return nil
		}

		return screen.Cmd(l.tickCmd(m.clock))
	}

	hovered := list.KeyAt(l.rows(m), l.Row)
	l.sources = msg.sources
	if l.listed {
		rows := l.rows(m)
		l.Row = cursor.Place(list.IndexOf(rows, hovered), l.Row, len(rows))

		return l.switchSource(m)
	}

	l.listed, l.err = true, nil
	l.Row = max(slices.IndexFunc(l.rows(m), func(row logSourceRow) bool { return row.failed }), 0)

	return l.switchSource(m)
}

func (m *model) applyLogsRead(msg logsReadMsg) tea.Cmd {
	l := &m.logs
	if msg.generation != l.generation {
		return nil
	}

	l.err = msg.err
	if msg.err != nil {
		return l.tickCmd(m.clock)
	}

	if msg.resp.GetReset_() || l.replacing {
		l.viewer.Reset()
	}
	l.replacing = false
	l.viewer.Append(msg.resp.GetLines())
	l.readCursor, l.read = msg.resp.GetCursor(), true

	if msg.resp.GetMore() {
		return l.readCmd(m)
	}

	return l.tickCmd(m.clock)
}

func (m *model) applyLogsTick(msg logsTickMsg) tea.Cmd {
	if msg.generation != m.logs.generation {
		return nil
	}

	if !m.logs.listed {
		return m.logs.listCmd(m)
	}

	return tea.Batch(m.logs.listCmd(m), m.logs.readCmd(m))
}

func (l *logsScreen) tickCmd(clock screen.Clock) tea.Cmd {
	generation := l.generation

	return clock.After(logsPollInterval, func(time.Time) tea.Msg { return logsTickMsg{generation: generation} })
}

// switchSource starts the block over on the source the cursor is on, unless it is the one being read.
func (l *logsScreen) switchSource(m *model) screen.Request {
	row, ok := list.At(l.rows(m), l.Row)
	if !ok || row.Key() == sourceKey(l.reading) {
		return nil
	}

	l.reading, l.logFocus = row.source, false
	l.logFilter, l.pattern = filterState{}, ""
	l.applyFormat(row)

	return screen.Batch{screen.Cmd(l.restart(m)), l.markFailedRunRead(m, row)}
}

// markFailedRunRead claims the agent's failed runs once the run that failed is opened, leaving a
// task's to the screen opening.
func (l *logsScreen) markFailedRunRead(m *model, row logSourceRow) screen.Request {
	if !row.failed || l.taskID != "" {
		return nil
	}

	agentID := l.agentID

	return m.write("marking the failed run read", "Could not mark the failed run read", func(a api) error { return a.markRunsRead(agentID) })
}

// restart drops the lines held and reads the source over again with the pattern the block is read with.
func (l *logsScreen) restart(m *model) tea.Cmd {
	l.generation++
	l.viewer.Reset()
	l.readCursor, l.read, l.err, l.replacing = "", false, nil, false

	return l.readCmd(m)
}

func (l *logsScreen) reread(m *model) tea.Cmd {
	l.generation++
	l.readCursor, l.err, l.replacing = "", nil, true

	return l.readCmd(m)
}

// openLogFilter hands the keys to the prompt at the foot of the log.
func (l *logsScreen) openLogFilter() {
	l.logFilter.typing = true
	l.logFilter.input.Insert()
	l.logFilter.mode = filterModeFilter
}

// updateLogFilter is the prompt at the foot of the log taking keys, reading the log over once the
// pattern sits still or the prompt is left.
func (l *logsScreen) updateLogFilter(m *model, key tea.KeyPressMsg) screen.Request {
	if key.String() == "ctrl+c" {
		return screen.Hide{}
	}

	if !l.logFilter.takeKey(key, m.register) {
		return nil
	}
	l.logFilter.mode = filterModeFilter
	if !l.logFilter.typing {
		return l.narrow(m)
	}

	l.edits++
	generation, edits := l.generation, l.edits

	return screen.Cmd(m.clock.After(logsFilterSettle, func(time.Time) tea.Msg { return logsSettledMsg{generation: generation, edits: edits} }))
}

func (m *model) applyLogsSettled(msg logsSettledMsg) screen.Request {
	l := &m.logs
	if msg.generation != l.generation || msg.edits != l.edits {
		return nil
	}

	return l.narrow(m)
}

// narrow reads the log over with the pattern typed, unless it is broken or already the one read with.
func (l *logsScreen) narrow(m *model) screen.Request {
	if l.logFilter.broken {
		return nil
	}

	pattern := ""
	if l.logFilter.pattern != nil {
		pattern = l.logFilter.pattern.String()
	}
	if pattern == l.pattern {
		return nil
	}

	l.pattern = pattern

	return screen.Cmd(l.reread(m))
}

// clearLogFilter drops the pattern standing over the log and reads it whole again.
func (l *logsScreen) clearLogFilter(m *model) screen.Request {
	l.logFilter.abandon()
	l.logFilter.mode = filterModeFilter

	return l.narrow(m)
}

func (l *logsScreen) update(m *model, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || l.viewer == nil {
		return nil
	}

	if l.logFilter.typing {
		return l.updateLogFilter(m, key)
	}

	if l.isBlockFocused(m) {
		return m.dispatchTop(l.actions(m), key.String())
	}

	return l.list().update(m, key, l.actions(m))
}

func (l *logsScreen) list() screenList[logSourceRow] {
	return screenList[logSourceRow]{
		cursor: func() *list.Cursor { return &l.Cursor },
		rows:   l.rows,
		name:   logSourceRowName,
		moved:  func(*model) screen.Request { return screen.Settle{} },
	}
}

func (l *logsScreen) rows(m *model) []logSourceRow {
	var failedRun *int32
	if item, ok := m.listing.AnyAgent(l.agentID); ok {
		failedRun = item.FailedRunLog
	}

	return narrowed(m.filter, narrowedParams[logSourceRow]{
		rows: xslice.Map(l.sources, func(source *logv1.LogSource) logSourceRow {
			return logSourceRow{
				source: source,
				name:   l.nameSource(m, source),
				failed: source.GetFailed() || (source.Run != nil && failedRun != nil && *source.Run == *failedRun),
			}
		}),
		depth: func(logSourceRow) int { return 0 },
		name:  logSourceRowName,
	})
}

func logSourceRowName(row logSourceRow) string {
	return row.name
}

// nameSource is what a source is called, led by the agent that ran it when the screen lists a task's runs.
func (l *logsScreen) nameSource(m *model, source *logv1.LogSource) string {
	if l.taskID == "" {
		return sourceLabel(source)
	}

	name := source.GetAgentId()
	if item, ok := m.listing.AnyAgent(name); ok {
		name = m.listing.AgentName(item.Agent)
	}

	return name + " " + sourceLabel(source)
}

func (l *logsScreen) listParams(m *model) screenListParams {
	return screenListParams{
		help:     l.help,
		empty:    l.emptyLine(),
		rows:     len(l.rows(m)),
		snapshot: func(pane list.Pane) view.Snapshot { return l.snapshot(m, pane) },
	}
}

// buildFrame fits the log's cursor and view to the block it is rendered in.
func (l *logsScreen) buildFrame(m *model) {
	split, ok := m.screenColumnSplit()
	if !ok || !l.read || l.viewer.Len() == 0 {
		return
	}

	l.viewer.Fit(split.column-len(list.Blank), max(split.rows-1, 1))
}

func (l *logsScreen) parent() screen.ID {
	return l.openedFrom
}

func (l *logsScreen) crumbs(m *model) crumbs {
	if task, ok := m.listing.Tasks[l.taskID]; ok {
		return crumbs{trail: []string{task.Slug, "logs"}}
	}

	name := l.agentID
	if item, ok := m.listing.AnyAgent(l.agentID); ok {
		name = m.listing.AgentName(item.Agent)
	}

	return crumbs{trail: []string{name, "logs"}}
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

func (l *logsScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		docRows: func(m *model, width int) []string {
			return m.logs.block(m, width)
		},
	}
}

// isBlockRendered reports whether the block beside the list holds lines of the log.
func (l *logsScreen) isBlockRendered(m *model) bool {
	if _, ok := m.screenColumnSplit(); !ok {
		return false
	}

	return l.read && l.viewer.Len() > 0
}

// isBlockFocused reports whether the log holds the keys.
func (l *logsScreen) isBlockFocused(m *model) bool {
	if !l.logFocus || !m.isListFocused() {
		return false
	}

	if _, ok := m.screenColumnSplit(); ok && l.logFilter.isStanding() {
		return true
	}

	return l.isBlockRendered(m)
}

func (l *logsScreen) focusBlock(*model) {
	l.logFocus = true
}

func (l *logsScreen) blurBlock() {
	l.logFocus = false
}

func (l *logsScreen) doesScrollColumn(m *model) bool {
	return l.isBlockRendered(m)
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

func (l *logsScreen) kinds(*model) stops {
	return stops{
		labels: logFormatLabels,
		focus:  noStop,
		lit:    int(l.format),
	}
}

// kindsFit renders the format strip only over a server log.
func (l *logsScreen) kindsFit(m *model) bool {
	row, ok := list.At(l.rows(m), l.Row)

	return ok && row.isServerLog() && m.listFitsTabs(l.listParams(m))
}

func (l *logsScreen) kindsFocused(*model) *bool {
	return &l.formatFocus
}

func (l *logsScreen) kindsSwitch(*model) bool {
	return true
}

// stepKind renders the server log in another format.
func (l *logsScreen) stepKind(m *model, delta int) screen.Request {
	l.format = logview.Format(stepStop(int(l.format), delta, len(logFormatLabels)))
	if row, ok := list.At(l.rows(m), l.Row); ok {
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
