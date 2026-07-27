package cli

import (
	"context"

	"claude-agents/config"
	"claude-agents/internal/agents"
	"claude-agents/internal/appconfig"
	"claude-agents/internal/store"
	agentstore "claude-agents/internal/store/agent"
	transcriptstore "claude-agents/internal/store/transcript"
	workflowstore "claude-agents/internal/store/workflow"
	"claude-agents/internal/tmux"
	"claude-agents/internal/ui"
)

// app is what every subcommand is wired from. Each command holds only the part
// it drives.
type app struct {
	// The services and their stores are built on demand by openStore, since only
	// the commands that serve or write need them. The panel is a client of the
	// service, so opening a pool it never queries would put a second writer on
	// the database.
	agentService    *agents.AgentService
	workflowService *agents.WorkflowService

	// The composition root. The pool is opened once and each domain gets its own
	// store over it, so what a service can reach is decided here. The pool is
	// handed on only as the transaction boundary for writes spanning two domains.
	pool        *store.Pool
	agents      *agentstore.Store
	workflows   *workflowstore.Store
	transcripts *transcriptstore.Store

	popup       *ui.Popup
	env         *config.Config
	keys        appconfig.Keybindings
	claudeFlags string
	// configErr is set when the user's config file would not parse. The panel shows
	// it on its error screen; every other command ignores it, since none of them
	// read keybindings.
	configErr error
	// tmux is shared with the services. The UI needs it directly for the stage,
	// which is its own layout rather than anything the agent API owns.
	tmux *tmux.Client
}

// openStore connects to the database and builds the services over it. Calling it
// twice is harmless, so a command can ask without knowing whether something
// already did.
func (a *app) openStore(ctx context.Context) error {
	if a.pool != nil {
		return nil
	}

	pool, err := store.Open(ctx, a.env.DatabaseURL)
	if err != nil {
		return err
	}

	a.pool = pool
	a.agents = agentstore.New(pool.DB())
	a.workflows = workflowstore.New(pool.DB())
	a.transcripts = transcriptstore.New(pool.DB())

	a.workflowService = agents.NewWorkflowService(agents.WorkflowParams{
		Workflows: a.workflows,
		Tx:        pool,
	})
	a.agentService = agents.NewAgentService(agents.AgentParams{
		Tmux:        a.tmux,
		Agents:      a.agents,
		Workflows:   a.workflows,
		Tx:          pool,
		Launcher:    agents.NewLauncher(a.tmux, a.env.Addr),
		ClaudeFlags: a.claudeFlags,
	})

	return nil
}
