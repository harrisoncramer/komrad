package ui

import (
	"fmt"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/logread"
	"komrad/internal/ui/logview"
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

// logsScreen tails the service log lines about one agent beside a sidebar naming it.
type logsScreen struct {
	agentID string
	// from is the view the screen was opened from.
	from   screen
	viewer logview.LogViewer
	cursor string
	// generation tells this visit's reads apart from an earlier one's.
	generation int
	read       bool
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

// enter starts the tail over, reading the newest lines about the agent the target names.
func (l *logsScreen) enter(_ host, s panelShared, target screenTarget) tea.Cmd {
	*l = logsScreen{
		agentID:    target.agentID,
		from:       target.from,
		viewer:     logview.NewTail(logsCeiling),
		generation: l.generation + 1,
	}

	return l.readCmd(s)
}

// leave ends the chain of reads.
func (l *logsScreen) leave() {
	l.generation++
}

func (l *logsScreen) readCmd(s panelShared) tea.Cmd {
	if l.agentID == "" {
		return nil
	}

	agentID, cursor, generation, clients := l.agentID, l.cursor, l.generation, s.api

	return func() tea.Msg {
		resp, err := clients.readAgentLogs(agentID, cursor, logsReadLimit)

		return logsReadMsg{generation: generation, resp: resp, err: err}
	}
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

	return m.logs.readCmd(m.shared())
}

func (l *logsScreen) tickCmd() tea.Cmd {
	generation := l.generation

	return tea.Tick(logsPollInterval, func(time.Time) tea.Msg { return logsTickMsg{generation: generation} })
}

func (l *logsScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || l.viewer == nil {
		return nil
	}

	pressed := key.String()
	if h.takeTopPress(pressed) {
		l.viewer.Top()

		return nil
	}
	if h.isTopHeld() {
		return nil
	}

	return l.actions().dispatch(h, s, pressed)
}

func (l *logsScreen) view(s panelShared) string {
	return s.draw.columnListView(screenListParams{
		help:  l.help,
		lines: func(listPane) ([]string, int) { return l.sidebarLines(s), 0 },
	})
}

// sidebarLines names the agent, what it is doing, and where the tail stands.
func (l *logsScreen) sidebarLines(s panelShared) []string {
	name, state := l.agentID, ""
	if item, ok := s.listing.anyAgent(l.agentID); ok {
		name, state = item.Agent.Name, strings.ReplaceAll(string(item.Agent.State), "_", " ")
	}

	lines := []string{name}
	if state != "" {
		lines = append(lines, dimStyle.Render(state))
	}
	if l.viewer == nil {
		return lines
	}

	following := "paused"
	if l.viewer.IsFollowing() {
		following = "following"
	}

	lines = append(lines, dimStyle.Render(following), dimStyle.Render(fmt.Sprintf("%d lines", l.viewer.Len())))
	if l.err != nil {
		lines = append(lines, "", dimStyle.Render("could not read: "+errui.GetReason(l.err)))
	}

	return lines
}

// rows is the log drawn at the width and height of the block beside the sidebar.
func (l *logsScreen) rows(m model, width int) []string {
	switch {
	case l.viewer != nil && !l.read && l.err != nil:
		return []string{dimStyle.Render("could not read: " + errui.GetReason(l.err))}
	case l.viewer == nil || !l.read:
		return []string{dimStyle.Render("reading…")}
	case l.viewer.Len() == 0:
		return []string{dimStyle.Render(fmt.Sprintf("Nothing about this agent in the last %d MB of the log.", logread.ScanBudget>>20))}
	}

	split, ok := m.screenColumnSplit()
	if !ok {
		return nil
	}

	return l.viewer.Rows(width, split.rows)
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
			return m.logs.rows(m, width)
		},
	}
}

func (l *logsScreen) doesScrollColumn(panelShared) bool {
	return l.viewer != nil
}

func (l *logsScreen) scrollColumn(_ host, _ panelShared, rows int) tea.Cmd {
	l.viewer.Move(rows)

	return nil
}

var (
	_ columned       = (*logsScreen)(nil)
	_ crumbed        = (*logsScreen)(nil)
	_ columnScroller = (*logsScreen)(nil)
)
