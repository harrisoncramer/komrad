// Package ui is the bubbletea panel for browsing and acting on Claude agents.
package ui

import (
	"fmt"
	"os"
	"strings"

	"claude-agents/internal/appconfig"
	"claude-agents/internal/errui"
	"claude-agents/proto/gen/agent/v1/agentv1connect"
	schedulev1connect "claude-agents/proto/gen/schedule/v1/schedulev1connect"
	"claude-agents/proto/gen/workflow/v1/workflowv1connect"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// Run launches the panel and blocks for the life of the persistent manager tmux
// session. Agents are read through the ConnectRPC client. The panel is the nav
// pane, the resource it hovers is shown in a sibling stage pane, and focus moves
// between the two with the user's own tmux bindings.
func Run(deps Deps) error {
	if _, err := tea.NewProgram(newModel(deps), tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("failed to run panel: %w", err)
	}

	return nil
}

type screen int

const (
	screenAgents screen = iota
	screenForm
	screenRename
	screenWorkflowForm
	screenWorkflowDelete
	screenSearch
	screenSchedules
	screenScheduleForm
	screenScheduleDelete
	screenTrash
	screenTrashDelete
)

// uiTmux is what the panel needs from tmux, enough to lay out its own panes plus
// the working directory of the client looking at it.
type uiTmux interface {
	stageTmux
	ClientPath(client string) string
}

// Deps are what the panel needs to run. The two services it acts through, the
// popup chrome it hides itself with, and a tmux client for its own layout.
type Deps struct {
	Popup     *Popup
	Tmux      uiTmux
	Agents    agentv1connect.AgentServiceClient
	Workflows workflowv1connect.WorkflowServiceClient
	Schedules schedulev1connect.ScheduleServiceClient
	Keys      appconfig.Keybindings
	// Marks are the marks the mark key steps an agent through, in the user's order.
	Marks []appconfig.Mark
	// Layout is how the popup's width is divided between the list and the stage.
	Layout appconfig.Layout
	// Addr is where Client points, shown on the error screen when the service
	// stops answering so the address to go looking at is on screen.
	Addr string
	// ConfigErr is set when the user's config file would not parse. The panel shows
	// it on its error screen instead of starting, so the message is readable rather
	// than written to a popup that closes on top of it. Keys fall back to the
	// built-in defaults, which is what the panel would have used anyway.
	ConfigErr error
	// StartupErr is set when the server the panel talks through could not be
	// started, most often because its database is not up. It is shown on the error
	// screen for the same reason ConfigErr is: this process is the command of the
	// tmux session behind the popup, so exiting takes the message down with it.
	StartupErr error
}

type model struct {
	popup *Popup
	// api is how the panel reads and changes anything. It is grouped so the
	// clients do not collide with the lists they populate.
	api api
	// tmux lays out the panel's own panes and answers which directory the user is
	// looking at, which is the one thing about a new agent the service cannot work
	// out for itself.
	tmux uiTmux
	// stage is the preview pane beside the list. It is the panel's own layout, so it
	// drives tmux directly. It fires on every cursor move, and a round trip per
	// keypress would be felt.
	stage *Stage

	// addr is where the service is expected to answer, shown when it does not.
	addr string
	// unreachable is the error from the last failed listing, nil while the service is
	// answering. The refresh keeps running while it is set, so the panel recovers on
	// its own once the service is back.
	unreachable error
	// epoch counts the writes this panel has made. A listing carries the epoch it was
	// requested under, so one that read the store before a write can be told from one
	// that read it after and dropped rather than putting the write's effect back.
	epoch int
	// pending is how many writes are in flight. While any is, the periodic refresh
	// stands down: a listing started now would read rows the write is about to
	// replace, and the write re-lists as it lands anyway.
	pending int
	// configErr is set when the config file would not parse. Unlike unreachable it
	// never clears, since the file is read once at startup, so the screen it shows
	// is the whole life of the process.
	configErr error
	// startupErr is set when the server could not be started. It never clears
	// either: nothing tries a second time, so a panel that came up without a server
	// has none for as long as it runs.
	startupErr error
	// actionErr is the last write the service refused, shown as a banner over the
	// screen it happened on. It is cleared when the next action is attempted and by
	// esc, never by a poll: a refused delete is still worth reading a second later.
	actionErr *errui.Banner

	screen screen

	// height is how tall the panel's own pane is, from the last resize. It is zero
	// until the first one arrives, which the lists read as unbounded rather than
	// clipping themselves against a height nothing measured.
	height int

	// width is how wide that pane is, read the same way and for the same reason. A
	// search snippet is clipped against it so a long one cannot push its own match
	// off the side.
	width int

	// The screens, each holding the rows it draws and where its cursor is. They are
	// grouped rather than flattened because the panel is five lists' worth of state and
	// a flat model made every field name carry the screen it belonged to.
	tree      treeState
	schedules scheduleState
	trash     trashState
	search    searchState

	// form is the open form, and it owns the input while it is up. values are what it
	// writes into and target is the record it is working on, both cleared as it closes.
	form   *huh.Form
	values formValues
	target formTarget

	keys  appconfig.Keybindings
	marks []appconfig.Mark
}

func newModel(deps Deps) model {
	m := model{
		api:        api{agents: deps.Agents, workflows: deps.Workflows, schedules: deps.Schedules},
		popup:      deps.Popup,
		tmux:       deps.Tmux,
		stage:      NewStage(deps.Tmux, deps.Layout.Split),
		addr:       deps.Addr,
		keys:       deps.Keys,
		marks:      deps.Marks,
		configErr:  deps.ConfigErr,
		startupErr: deps.StartupErr,
		screen:     screenAgents,
	}

	// A failure that came up with the process shows its error in place of the panel,
	// so none of the panel's setup runs: no stage pane is split and nothing is
	// listed. The screen is all this process does.
	if m.fatal() != nil {
		return m
	}

	// Split off the stage pane beside the panel. There is nothing to stage in it yet:
	// the first listing is a command like every other one, so the panel paints its
	// empty tree now rather than holding the first frame back for a round trip.
	m.stage.Init()

	return m
}

// currentDir fills in the directory a new agent starts in. The form allows a
// blank, meaning "where I am", and only the panel can answer that, since it is the
// invoking tmux client's path. The API refuses a blank directory because a server
// has no way to know which pane the user is looking at.
func (m model) currentDir(dir string) string {
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		return trimmed
	}
	if path := m.tmux.ClientPath(m.popup.InvokingClient()); path != "" {
		return path
	}

	home, _ := os.UserHomeDir()

	return home
}

// clampCursor keeps an index within a list of the given length.
func clampCursor(idx, length int) int {
	return max(min(idx, length-1), 0)
}
