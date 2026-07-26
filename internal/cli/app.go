package cli

import (
	"context"

	"claude-agents/config"
	"claude-agents/internal/agents"
	"claude-agents/internal/appconfig"
	"claude-agents/internal/store"
	sessionstore "claude-agents/internal/store/session"
	transcriptstore "claude-agents/internal/store/transcript"
	workflowstore "claude-agents/internal/store/workflow"
	"claude-agents/internal/tmux"
	"claude-agents/internal/ui"
)

// app is what every subcommand is wired from. The services are separate rather
// than one object, so each command holds only the part it drives.
type app struct {
	// The services and the stores they run over are built on demand by
	// openStore, since only the commands that serve or write need them. The
	// panel does not: it is a client of the service, and opening a connection
	// pool it never queries would put a second writer back on the database.
	sessions  *agents.SessionService
	workflows *agents.WorkflowService

	// This is the composition root: the pool is opened once and each domain gets
	// its own small store over it, so what a service can reach is decided here
	// and visible in how it was built. The pool itself is handed on only as the
	// transaction boundary, for the writes that span two domains.
	pool        *store.Pool
	chats       *sessionstore.Store
	buckets     *workflowstore.Store
	transcripts *transcriptstore.Store

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
	if a.pool != nil {
		return nil
	}

	pool, err := store.Open(ctx, a.env.DatabaseURL)
	if err != nil {
		return err
	}

	a.pool = pool
	a.chats = sessionstore.New(pool.DB())
	a.buckets = workflowstore.New(pool.DB())
	a.transcripts = transcriptstore.New(pool.DB())

	a.workflows = agents.NewWorkflowService(agents.WorkflowParams{
		Workflows: a.buckets,
		Tx:        pool,
	})
	a.sessions = agents.NewSessionService(agents.SessionParams{
		Tmux:        a.tmux,
		Sessions:    a.chats,
		Workflows:   a.buckets,
		Tx:          pool,
		Launcher:    agents.NewLauncher(a.tmux, a.env.Addr),
		ClaudeFlags: a.claudeFlags,
	})

	return nil
}
