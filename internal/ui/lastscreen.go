package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/savedscreen"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// errNoSavedScreen is an agent whose pane was never saved.
var errNoSavedScreen = errors.New("no saved screen")

// lastScreen is the saved screen of the agent the column is drawn for.
type lastScreen struct {
	agentID  string
	answered bool
	screen   *agentv1.GetLastScreenResponse
	err      error
}

type lastScreenMsg struct {
	agentID string
	screen  *agentv1.GetLastScreenResponse
	err     error
}

// screenedAgentID is the agent whose saved screen the column draws.
func (m model) screenedAgentID() (string, bool) {
	s := m.shared()

	switch m.screen {
	case screenArchive:
		if m.archive.kind != archiveAgents {
			return "", false
		}

		agent, ok := m.archive.hoveredAgent(s)

		return agent.ID, ok
	case screenAgents:
		item, ok := m.agents.hoveredResting(s)

		return item.Agent.ID, ok
	case screenWorktrees:
		row, ok := m.worktrees.hovered(s)
		if !ok || row.item.agent == nil {
			return "", false
		}

		if live, ok := s.listing.liveAgent(row.item.agent.id); ok && live.Alive {
			return "", false
		}

		return row.item.agent.id, true
	case screenSchedules, screenSearch, screenReview, screenDocuments, screenLogs, screenDocumentTypes:
	}

	return "", false
}

func (m model) isScreenShown() bool {
	_, ok := m.screenedAgentID()

	return ok
}

func (m *model) readLastScreenCmd() tea.Cmd {
	agentID, ok := m.screenedAgentID()
	if !ok {
		return nil
	}

	m.lastScreen = lastScreen{agentID: agentID}
	clients := m.api

	return func() tea.Msg {
		screen, err := clients.getLastScreen(agentID)

		return lastScreenMsg{
			agentID: agentID,
			screen:  screen,
			err:     err,
		}
	}
}

func (m *model) readMovedLastScreenCmd() tea.Cmd {
	agentID, ok := m.screenedAgentID()
	if !ok {
		m.lastScreen = lastScreen{}

		return nil
	}

	if agentID == m.lastScreen.agentID {
		return nil
	}

	return m.readLastScreenCmd()
}

func (m *model) applyLastScreen(msg lastScreenMsg) {
	if msg.agentID != m.lastScreen.agentID {
		return
	}

	m.lastScreen = lastScreen{
		agentID:  msg.agentID,
		answered: true,
		screen:   msg.screen,
		err:      msg.err,
	}
}

type lastScreenRowsParams struct {
	screen  lastScreen
	agentID string
	width   int
	height  int
}

// lastScreenRows is the saved screen under a line saying when it was taken, fitted to the
// column, or one line saying why there is none.
func lastScreenRows(params lastScreenRowsParams) []string {
	muted := paint.Panel.Style(paint.ToneMuted)
	shown := params.screen

	switch {
	case shown.agentID != params.agentID || !shown.answered:
		return []string{muted.Render("reading the saved screen…")}
	case errors.Is(shown.err, errNoSavedScreen):
		return []string{muted.Render("no saved screen")}
	case shown.err != nil:
		return []string{muted.Render("could not read the saved screen: " + errui.GetReason(shown.err))}
	}

	saved := shown.screen
	banner := muted.Render(strings.Join([]string{
		"saved screen",
		label.Friendly(saved.GetCapturedAt().AsTime()),
		fmt.Sprintf("%dx%d", saved.GetWidth(), saved.GetHeight()),
	}, separated))
	height := min(max(params.height-1, 1), int(saved.GetHeight()))

	return append([]string{banner}, strings.Split(savedscreen.Fit(saved.GetFrame(), max(params.width, 1), height), "\n")...)
}

func noticeRows(notice string, width int) []string {
	if notice == "" {
		return nil
	}

	const padding = 2

	text := ansi.Truncate(notice, max(width-2*padding, 1), "…")
	palette := paint.Panel
	fill := lipgloss.NewStyle().
		Background(palette.Color(paint.ToneModeVisual)).
		Foreground(palette.Color(paint.ToneInverse)).
		Width(min(ansi.StringWidth(text)+2*padding, max(width, 1)))
	bar := fill.Render("")

	return []string{bar, fill.Bold(true).PaddingLeft(padding).Render(text), bar}
}

func (m model) screenNotice() string {
	s := m.shared()

	switch m.screen {
	case screenArchive:
		restore := m.keys.ArchiveMenu.Restore
		if target, ok := m.archive.menuTarget(s); ok {
			if _, err := target.opensAgent(); err == nil {
				restore = m.keys.ArchiveMenu.RestoreOpen
			}
		}

		return "archived" + separated + "press " + keyLabel(m.keys.Menu.Open) + " " + keyLabel(m.keys.Menu.Archive) + " " + keyLabel(restore) + " to restore"
	case screenAgents:
		item, ok := m.agents.hoveredResting(s)
		if ok && item.Agent.HeldAt != nil {
			return "paused" + separated + "press " + keyLabel(m.keys.Agents.Pause) + " to unpause"
		}

		return "dormant" + separated + "press " + keyLabel(m.keys.Agents.Open) + " to restore"
	case screenWorktrees:
		return "not running"
	case screenSchedules, screenSearch, screenReview, screenDocuments, screenLogs, screenDocumentTypes:
	}

	return ""
}

// columnHeight is the rows the block beside the list is drawn in.
func (m model) columnHeight() int {
	split, ok := m.columnSplit()
	if !ok {
		return 0
	}

	return split.rows
}

// minScreenRows is the fewest rows a saved screen is fitted to under its document before it is
// given the whole column to scroll to instead.
const minScreenRows = 8

// withLastScreen is a column's rows under a notice saying the agent is not running, with the
// saved screen of the agent it screens drawn under them.
func (m model) withLastScreen(rows []string, width int) []string {
	agentID, ok := m.screenedAgentID()
	if !ok {
		return rows
	}

	top := noticeRows(m.screenNotice(), width)
	if slices.ContainsFunc(rows, func(row string) bool { return strings.TrimSpace(ansi.Strip(row)) != "" }) {
		top = slices.Concat(top, []string{""}, rows)
	}

	height := m.columnHeight() - len(top) - 1
	if height-1 < minScreenRows {
		height = m.columnHeight()
	}

	return slices.Concat(top, []string{""}, lastScreenRows(lastScreenRowsParams{
		screen:  m.lastScreen,
		agentID: agentID,
		width:   width,
		height:  height,
	}))
}
