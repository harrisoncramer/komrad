package cli

import (
	"context"

	"claude-agents/config"
	"claude-agents/internal/agents"
	"claude-agents/internal/appconfig"
	"claude-agents/internal/store"
	"claude-agents/internal/tmux"
	"claude-agents/internal/ui"
)

// app is what every subcommand is wired from. The services are separate rather
// than one object, so each command holds only the part it drives.
type app struct {
	// The services and the store they run over are built on demand by
	// openStore, since only the commands that serve or write need them. The
	// panel does not: it is a client of the service, and opening a connection
	// pool it never queries would put a second writer back on the database.
	sessions  *agents.SessionService
	workflows *agents.WorkflowService
	store     *store.Store

	popup       *ui.Popup
	env         *config.Config
	keys        appconfig.Keybindings
	claudeFlags string
	// tmux is shared with the services. The UI needs it directly for the stage,
	// which is its own layout rather than anything the session API owns.
	tmux *tmux.Client
}

// openStore connects to the database and builds the services over it. Calling it
// twice is harmless, so a command can ask without knowing whether something
// already did.
func (a *app) openStore(ctx context.Context) error {
	if a.store != nil {
		return nil
	}

	st, err := store.Open(ctx, a.env.DatabaseURL)
	if err != nil {
		return err
	}

	a.store = st
	a.workflows = agents.NewWorkflowService(st)
	a.sessions = agents.NewSessionService(agents.SessionParams{
		Tmux:        a.tmux,
		Store:       st,
		Launcher:    agents.NewLauncher(a.tmux, a.env.Addr),
		ClaudeFlags: a.claudeFlags,
	})

	return nil
}
