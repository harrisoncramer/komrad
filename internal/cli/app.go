package cli

import (
	"context"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/appconfig"
	"weave/internal/harness"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/tmux"
	"weave/internal/ui"
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
	scheduleService *agents.ScheduleService

	// The composition root. The pool is opened once and each domain gets its own
	// store over it, so what a service can reach is decided here. The pool is
	// handed on only as the transaction boundary for writes spanning two domains.
	pool        *store.Pool
	agents      *agentstore.Store
	workflows   *workflowstore.Store
	schedules   *schedulestore.Store
	transcripts *transcriptstore.Store

	popup *ui.Popup
	env   *config.Config
	keys  appconfig.Keybindings
	// harnesses is every agent CLI this binary can run, which the services resolve an
	// agent's own harness through.
	harnesses *harness.Registry
	// defaultHarness is the CLI an agent runs when neither it nor its workflow names
	// one, and defaultFlags are the configured flags each harness's agents inherit,
	// keyed by harness id.
	defaultHarness string
	defaultFlags   map[string]string
	// marks are the marks the panel's mark key steps an agent through.
	marks []appconfig.Mark
	// layout is how the panel divides the popup between its list and the stage.
	layout appconfig.Layout
	// configErr is set when the user's config file would not parse. The panel shows
	// it on its error screen; every other command ignores it, since none of them
	// read keybindings.
	configErr error
	// configPath is the file the user pointed at, empty for the default location.
	// Serve keeps the path rather than the config parsed from it, since it reads the
	// file again per request.
	configPath string
	// tmux is shared with the services. The UI needs it directly for the stage,
	// which is its own layout rather than anything the agent API owns. It covers
	// both tmux servers, sending each call to the one that can answer it.
	tmux *tmux.Servers
	// host is the user's tmux server, kept alongside so the open command can record
	// which one opened the popup. Every other use of it goes through tmux.
	host *tmux.Host
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
	a.schedules = schedulestore.New(pool.DB())
	a.transcripts = transcriptstore.New(pool.DB())

	a.workflowService = agents.NewWorkflowService(agents.WorkflowParams{
		Workflows: a.workflows,
		Schedules: a.schedules,
		Tx:        pool,
	})
	a.scheduleService = agents.NewScheduleService(agents.ScheduleParams{
		Schedules: a.schedules,
		Workflows: a.workflows,
	})
	a.agentService = agents.NewAgentService(agents.AgentParams{
		Tmux:           a.tmux,
		Agents:         a.agents,
		Workflows:      a.workflows,
		Tx:             pool,
		Launcher:       agents.NewLauncher(a.tmux, a.env.Addr),
		Harnesses:      a.harnesses,
		DefaultHarness: a.defaultHarness,
		DefaultFlags:   a.defaultFlags,
	})

	return nil
}
