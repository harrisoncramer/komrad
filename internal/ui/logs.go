package ui

import (
	"fmt"
	"slices"
	"time"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/logread"
	"komrad/internal/ui/logview"
	"komrad/internal/ui/paint"
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
)

// logsScreen lists where one agent's logs can be read from and draws the newest lines of the
// source under the cursor beside the list.
type logsScreen struct {
	rowCursor
	agentID string
	// from is the view the screen was opened from.
	from    screen
	sources []*logv1.LogSource
	listed  bool
	// reading is the id of the source the block holds lines of.
	reading string
	viewer  logview.LogViewer
	cursor  string
	// generation tells this visit's and this source's reads apart from an earlier one's.
	generation int
	read       bool
	err        error
	// logFocus says the cycle key handed the keys to the log.
	logFocus bool
}

// logSourceRow is one line of the list of log sources.
type logSourceRow struct {
	source *logv1.LogSource
	failed bool
}

func (r logSourceRow) key() string {
	return "log:" + r.source.GetId()
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

// enter starts over on the agent the target names, listing where its logs can be read from.
func (l *logsScreen) enter(_ host, s panelShared, target screenTarget) tea.Cmd {
	*l = logsScreen{
		agentID:    target.agentID,
		from:       target.from,
		viewer:     logview.NewTail(logsCeiling),
		generation: l.generation + 1,
	}

	return l.listCmd(s)
}

// leave ends the chain of reads.
func (l *logsScreen) leave() {
	l.generation++
}

func (l *logsScreen) listCmd(s panelShared) tea.Cmd {
	if l.agentID == "" {
		return nil
	}

	agentID, generation, clients := l.agentID, l.generation, s.api

	return func() tea.Msg {
		sources, err := clients.listAgentLogSources(agentID)

		return logsSourcesMsg{generation: generation, sources: sources, err: err}
	}
}

func (l *logsScreen) readCmd(s panelShared) tea.Cmd {
	row, ok := rowAt(l.rows(s), l.rowCursor.cursor)
	if !ok {
		return nil
	}

	agentID, source, cursor, generation, clients := l.agentID, row.source.GetId(), l.cursor, l.generation, s.api

	return func() tea.Msg {
		resp, err := clients.readAgentLogs(agentID, source, cursor, logsReadLimit)

		return logsReadMsg{generation: generation, resp: resp, err: err}
	}
}

// applyLogsSources takes a listing of the sources, landing the first on the agent's failed run
// and keeping the cursor on the source it was on after that.
func (m *model) applyLogsSources(msg logsSourcesMsg) tea.Cmd {
	l := &m.logs
	if msg.generation != l.generation {
		return nil
	}

	if msg.err != nil {
		l.err = msg.err
		if l.listed {
			return nil
		}

		return l.tickCmd()
	}

	s := m.shared()
	hovered := rowKeyAt(l.rows(s), l.rowCursor.cursor)
	l.sources = msg.sources
	if l.listed {
		l.rowCursor.cursor = placeCursor(rowIndexOfKey(l.rows(s), hovered), l.rowCursor.cursor, len(l.sources))

		return l.switchSource(s)
	}

	l.listed, l.err = true, nil
	l.rowCursor.cursor = max(slices.IndexFunc(l.rows(s), func(row logSourceRow) bool { return row.failed }), 0)

	return l.switchSource(s)
}

func (m *model) applyLogsRead(msg logsReadMsg) tea.Cmd {
	l := &m.logs
	if msg.generation != l.generation {
		return nil
	}

	l.err = msg.err
	if msg.err != nil {
		return l.tickCmd()
	}

	if msg.resp.GetReset_() {
		l.viewer.Reset()
	}
	l.viewer.Append(msg.resp.GetLines())
	l.cursor, l.read = msg.resp.GetCursor(), true

	if msg.resp.GetMore() {
		return l.readCmd(m.shared())
	}

	return l.tickCmd()
}

func (m *model) applyLogsTick(msg logsTickMsg) tea.Cmd {
	if msg.generation != m.logs.generation {
		return nil
	}

	s := m.shared()
	if !m.logs.listed {
		return m.logs.listCmd(s)
	}

	return tea.Batch(m.logs.listCmd(s), m.logs.readCmd(s))
}

func (l *logsScreen) tickCmd() tea.Cmd {
	generation := l.generation

	return tea.Tick(logsPollInterval, func(time.Time) tea.Msg { return logsTickMsg{generation: generation} })
}

// switchSource starts the block over on the source the cursor is on, unless it is the one being read.
func (l *logsScreen) switchSource(s panelShared) tea.Cmd {
	row, ok := rowAt(l.rows(s), l.rowCursor.cursor)
	if !ok || row.source.GetId() == l.reading {
		return nil
	}

	l.reading = row.source.GetId()
	l.generation++
	l.viewer.Reset()
	l.cursor, l.read, l.err, l.logFocus = "", false, nil, false

	return l.readCmd(s)
}

func (l *logsScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || l.viewer == nil {
		return nil
	}

	if l.isLogFocused(s) {
		return l.actions(s).dispatch(h, s, key.String())
	}

	return l.list().update(h, s, key, l.actions(s))
}

func (l *logsScreen) list() screenList[logSourceRow] {
	return screenList[logSourceRow]{
		cursor: func() *rowCursor { return &l.rowCursor },
		rows:   l.rows,
		moved: func(_ host, s panelShared) tea.Cmd {
			return l.switchSource(s)
		},
		quit: func(k config.Keybindings) []string { return k.Logs.Quit },
		back: func(k config.Keybindings) []string { return k.Logs.Back },
	}
}

func (l *logsScreen) rows(s panelShared) []logSourceRow {
	var failedRun *int32
	if item, ok := s.listing.anyAgent(l.agentID); ok {
		failedRun = item.FailedRunLog
	}

	return xslice.Map(l.sources, func(source *logv1.LogSource) logSourceRow {
		return logSourceRow{
			source: source,
			failed: source.Run != nil && failedRun != nil && *source.Run == *failedRun,
		}
	})
}

func (l *logsScreen) view(s panelShared) string {
	return s.draw.columnListView(screenListParams{
		help:  l.help,
		empty: l.emptyLine(),
		rows:  len(l.sources),
		lines: func(pane listPane) ([]string, int) { return l.lines(s, pane), l.rowCursor.cursor },
	})
}

func (l *logsScreen) emptyLine() string {
	if l.err != nil && !l.listed {
		return "could not list: " + errui.GetReason(l.err)
	}

	return "reading…"
}

// lines is the list as drawn: each source's name behind the cursor, the failed run marked after it.
func (l *logsScreen) lines(s panelShared, pane listPane) []string {
	rows := l.rows(s)
	if len(rows) == 0 {
		return []string{dimStyle.Render("  " + l.emptyLine())}
	}

	caret := pane.caret
	if l.isLogFocused(s) {
		caret = view.CaretDrawn
	}

	return xslice.Map(rows, func(row logSourceRow) string {
		hovered := row.key() == rowKeyAt(rows, l.rowCursor.cursor)
		prefix := "  "
		if hovered {
			prefix = caretGlyph(caret)
		}

		line := prefix + " " + nameStyle(hovered && caret != view.CaretAbsent, false).Render(clipName(row.source.GetName()))
		if row.failed {
			line += " " + paint.Panel.Style(paint.ToneDanger).Render(s.glyphs.Spell(glyph.FailedScript))
		}

		return line
	})
}

// block is the log drawn at the width and height of the block beside the sidebar.
func (l *logsScreen) block(m model, width int) []string {
	row, hovered := rowAt(l.rows(m.shared()), l.rowCursor.cursor)

	switch {
	case !hovered:
		return nil
	case !l.read && l.err != nil:
		return []string{dimStyle.Render("could not read: " + errui.GetReason(l.err))}
	case !l.read:
		return []string{dimStyle.Render("reading…")}
	case l.viewer.Len() == 0 && row.source.Run != nil:
		return []string{dimStyle.Render("This run wrote nothing.")}
	case l.viewer.Len() == 0:
		return []string{dimStyle.Render(fmt.Sprintf("Nothing about this agent in the last %d MB of the log.", logread.ScanBudget>>20))}
	}

	split, ok := m.screenColumnSplit()
	if !ok {
		return nil
	}

	rows, cursor := l.viewer.Rows(width-len(blankCursor), split.rows)
	focused := l.isLogFocused(m.shared())
	for i, row := range rows {
		prefix := blankCursor
		if focused && i == cursor {
			prefix = caretGlyph(m.blinkingCaret(true))
		}
		rows[i] = prefix + row
	}

	return rows
}

func (l *logsScreen) parent() screen {
	return l.from
}

func (l *logsScreen) crumbs(s panelShared) crumbs {
	name := l.agentID
	if item, ok := s.listing.anyAgent(l.agentID); ok {
		name = item.Agent.Name
	}

	return crumbs{trail: []string{name, "logs"}}
}

func (l *logsScreen) doesDrawFromListing() bool {
	return true
}

func (l *logsScreen) takesEveryKey() bool {
	return false
}

func (l *logsScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		docRows: func(m model, width int) []string {
			return m.logs.block(m, width)
		},
	}
}

// isLogDrawn reports whether the block beside the list holds lines of the log.
func (l *logsScreen) isLogDrawn(s panelShared) bool {
	if _, ok := s.draw.screenColumnSplit(); !ok {
		return false
	}

	return l.read && l.viewer.Len() > 0
}

// isLogFocused reports whether the log holds the keys.
func (l *logsScreen) isLogFocused(s panelShared) bool {
	return l.logFocus && l.isLogDrawn(s) && s.draw.isListFocused()
}

func (l *logsScreen) doesScrollColumn(s panelShared) bool {
	return l.isLogDrawn(s)
}

func (l *logsScreen) scrollColumn(_ host, _ panelShared, rows int) tea.Cmd {
	l.viewer.Scroll(rows)

	return nil
}

// move moves the log's cursor a number of lines toward the newest, or toward the oldest for a negative count.
func (l *logsScreen) move(lines int) tea.Cmd {
	l.viewer.Move(lines)

	return nil
}

var (
	_ columned       = (*logsScreen)(nil)
	_ crumbed        = (*logsScreen)(nil)
	_ columnScroller = (*logsScreen)(nil)
)
