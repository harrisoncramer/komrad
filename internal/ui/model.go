// Package ui is the bubbletea panel for browsing and acting on background agents.
package ui

import (
	"fmt"
	"os"
	"strings"

	"weave/internal/appconfig"
	"weave/internal/errui"
	"weave/proto/gen/action/v1/actionv1connect"
	"weave/proto/gen/agent/v1/agentv1connect"
	schedulev1connect "weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// Run launches the panel and blocks for the life of the persistent manager tmux session.
// The panel is the nav pane, the resource it hovers is shown in a sibling stage pane, and
// focus moves between the two with the user's own tmux bindings.
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
	screenWorkflowArchive
	screenSearch
	screenSchedules
	screenScheduleForm
	screenScheduleArchive
	screenArchive
	screenArchiveDelete
	screenActions
)

// uiTmux is what the panel needs from tmux: enough to lay out its own panes, the directory
// the client looking at it sits in, the session a window opened for the user has to land
// in, and the options an action reads to find the panes the panel laid out.
type uiTmux interface {
	stageTmux
	GetClientPath(client string) string
	SetPanelGlobal(name, value string)
	OpenWindow(client, name, dir string, argv ...string) error
}

// Deps are what the panel needs to run.
type Deps struct {
	PopupWindow *PopupWindow
	Tmux        uiTmux
	Agents      agentv1connect.AgentServiceClient
	Workflows   workflowv1connect.WorkflowServiceClient
	Schedules   schedulev1connect.ScheduleServiceClient
	Actions     actionv1connect.ActionServiceClient
	Keys        appconfig.Keybindings
	// Marks are the marks the mark key steps an agent through, in the user's order.
	Marks []appconfig.Mark
	// Layout is how the popup's width is divided between the list and the stage, and how
	// much of the stage's height the pane under it takes when something opens there.
	Layout appconfig.Layout
	// Addr is where Client points, shown on the error screen when the service stops
	// answering.
	Addr string
	// LogViewer is the command the log key opens the log in, from the user's config. It
	// runs through a shell, so it can be a pipeline.
	LogViewer string
	// Binary is this executable, which a viewer command reaches through WEAVE_BIN. It is
	// passed in so the panel and the service it spawns are the same build.
	Binary string
	// LogPath is the file the service writes to, which the log key pages. It is passed in
	// so the panel reads the file the process that spawned the service pointed it at.
	LogPath string
	// ConfigErr is set when the user's config file would not parse. The panel shows it on
	// its error screen instead of starting, since a message written to a popup that closes
	// on top of it cannot be read.
	ConfigErr error
	// StartupErr is set when the server the panel talks through could not be started, most
	// often because its database is not up. It shows on the error screen for the reason
	// ConfigErr does: exiting takes the message down with the popup.
	StartupErr error
	// Pane is the pane the panel runs in, read from the environment tmux gives that pane.
	// The stage lays the window out around it, so it has to be this pane and not whichever
	// one an attached client happens to be looking at.
	Pane string
}

type model struct {
	popup *PopupWindow
	api   api
	// tmux answers which directory the user is looking at, which is the one thing about a
	// new agent the service cannot work out for itself.
	tmux uiTmux
	// stage is the preview pane beside the list. It is the panel's own layout, so it
	// drives tmux directly rather than paying a round trip per keypress.
	stage *Stage

	addr string
	// unreachable is the error from the last failed listing, nil while the service is
	// answering. The refresh keeps running while it is set, so the panel recovers on its
	// own once the service is back.
	unreachable error
	// epoch counts the writes this panel has made. A listing carries the epoch it was
	// requested under, so one that read the store before a write is dropped rather than
	// putting the write's effect back.
	epoch int
	// pending is how many writes are in flight. While any is, the periodic refresh stands
	// down: it would read rows the write is about to replace, and the write re-lists as it
	// lands anyway.
	pending int
	// configErr and startupErr never clear, unlike unreachable, since the config is read
	// once at startup and nothing retries the server.
	configErr  error
	startupErr error
	// banner is the last write the service refused, shown over the screen it happened on.
	// It is cleared by the next write and by esc, never by a poll, since a refused delete
	// is still worth reading a second later.
	banner *errui.Banner

	screen screen

	// height and width are the panel's own pane, from the last resize. They are zero until
	// the first one arrives, which the lists read as unbounded rather than clipping
	// against a size nothing measured.
	height int
	width  int

	// The screens, each holding the rows it draws and where its cursor is.
	tree      treeState
	schedules scheduleState
	archive   archiveState
	search    searchState
	actions   actionState

	// filter narrows whichever of those lists is on screen. It is one state rather than one
	// per screen, since only the list being looked at can be narrowed and moving to another
	// takes the pattern off.
	filter filterState

	// form is the open form, and it owns the input while it is up. values are what it
	// writes into and target is the record it is working on, both cleared as it closes.
	form   *huh.Form
	values formValues
	target formTarget
	// pendingForm is whether the open form is waiting on the write it submitted. It stays
	// up until that write lands, so a refusal is answered on the fields as filled in.
	pendingForm bool

	// helpExpanded is whether the help under a list names every binding rather than the one
	// line it opens as, which the help key toggles.
	helpExpanded bool

	// topHeld is whether the jump-to-top key has been pressed once and is waiting on the
	// second press that acts, the way vim's gg waits. Any other key drops it.
	topHeld bool

	keys  appconfig.Keybindings
	marks []appconfig.Mark
	// logPath is the service's log, logViewer is what the log key opens it in, and binary
	// is this executable, which the viewer reaches through the environment.
	logPath   string
	logViewer string
	binary    string
}

func newModel(deps Deps) model {
	m := model{
		api:        api{agents: deps.Agents, workflows: deps.Workflows, schedules: deps.Schedules, actions: deps.Actions},
		popup:      deps.PopupWindow,
		tmux:       deps.Tmux,
		stage:      NewStage(deps.Tmux, deps.Layout),
		addr:       deps.Addr,
		logPath:    deps.LogPath,
		logViewer:  deps.LogViewer,
		binary:     deps.Binary,
		keys:       deps.Keys,
		marks:      deps.Marks,
		configErr:  deps.ConfigErr,
		startupErr: deps.StartupErr,
		screen:     screenAgents,
	}

	// A failure that came up with the process shows its error in place of the panel, so
	// none of the panel's setup runs. The screen is all this process does.
	if m.fatal() != nil {
		return m
	}

	// There is nothing to stage yet: the first listing is a command like any other, so the
	// panel paints its empty tree rather than holding the first frame back for a round
	// trip.
	m.stage.Init(deps.Pane)

	return m
}

// currentDir fills in the directory a new agent starts in. The form allows a blank,
// meaning "where I am", which only the panel can answer since it is the invoking client's
// path.
func (m model) currentDir(dir string) string {
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		return trimmed
	}
	if path := m.tmux.GetClientPath(m.popup.InvokingClient()); path != "" {
		return path
	}

	home, _ := os.UserHomeDir()

	return home
}

// clampCursor keeps an index within a list of the given length.
func clampCursor(idx, length int) int {
	return max(min(idx, length-1), 0)
}
