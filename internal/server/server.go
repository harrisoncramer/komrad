// Package server assembles the service: the stores, the domain services over them, the
// handlers those are served through, and the background work that keeps what the panel
// shows correct. It starts and stops that work itself, so the same service a user runs can
// be stood up inside a test.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"weave/config"
	"weave/db"
	"weave/internal/actions"
	"weave/internal/agents"
	"weave/internal/harness"
	"weave/internal/interceptors"
	"weave/internal/jobs"
	"weave/internal/mcp"
	rpcaction "weave/internal/rpc/action"
	rpcagent "weave/internal/rpc/agent"
	rpcconfig "weave/internal/rpc/config"
	rpcschedule "weave/internal/rpc/schedule"
	rpcworkflow "weave/internal/rpc/workflow"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	schedulestore "weave/internal/store/schedule"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/tmux"
	"weave/proto/gen/action/v1/actionv1connect"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/config/v1/configv1connect"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// ReconcileInterval is how often the service re-observes every agent and writes its state.
// It stays brisk enough that starting or finishing work shows within a couple of seconds,
// while the tmux captures stay cheap.
const ReconcileInterval = 2 * time.Second

// ScheduleReconcileInterval is how often the service re-reads the schedules table and
// makes River's periodic jobs match it, since River holds those in memory. It is slow
// because a schedule is a record of future work, where the same lag on agent state would
// show.
const ScheduleReconcileInterval = 10 * time.Second

// MailDeliveryInterval is how often the service looks for mail somebody has not read. It is
// brisk because it is the path every piece of mail takes: a pass asks the database which
// mailboxes hold anything, which is an indexed read that usually finds nothing.
const MailDeliveryInterval = 2 * time.Second

// TranscriptSyncInterval is how often the service folds new transcript text into the
// search index. A stale search corpus is harmless where a stale state count is not, and
// each pass re-indexes potentially large logs, so it stays off the hot path.
const TranscriptSyncInterval = 15 * time.Second

// BranchSyncInterval is how often the service re-reads the branch checked out at every
// live agent's cwd. A stale branch label is harmless the way a stale search corpus is, and
// each pass is a git shell-out per agent rather than a tmux capture, which is why it runs
// on its own slow pass instead of inside the two-second reconcile loop.
const BranchSyncInterval = 15 * time.Second

// Params is everything the service is built from. Every one of them is gathered before
// this is reached, so nothing here reads a file, an environment, or a flag.
type Params struct {
	// Env is the process configuration the connection strings and paths come from.
	Env *config.Environment
	// ConfigPaths is the user's config layers, weakest first, and empty for the default
	// path. The paths are kept rather than the config parsed from them, since the config
	// service and the action service both read the files per request.
	ConfigPaths []string
	// Logger is what the served handlers write their request lines to.
	Logger *slog.Logger
	// Addr is where the process listens, which agents are told so they can call back.
	Addr string
	// Tmux is both tmux servers as one client, where agent panes are opened and read.
	Tmux *tmux.Servers
	// Harnesses is every agent CLI this build can run.
	Harnesses *harness.Registry
	// DefaultHarness is the CLI an agent runs when neither it nor its workflow names one.
	DefaultHarness string
	// DefaultFlags are the configured flags each harness's agents inherit, by harness id.
	DefaultFlags map[string]string
	// MaxAgents is how many live agents the panel holds at once.
	MaxAgents int
	// ContextPath is the file holding the standing text every agent is started with.
	ContextPath string
	// HookCommand is what an agent CLI's hooks run to reach the panel, which is this
	// binary's own path. Empty launches agents with no hooks registered, so nothing
	// delivers their mail.
	HookCommand string
	// FallbackResumeRoot is where an agent runs when the directory it was created in cannot be
	// brought back. Empty takes the default beside the user's own agent state.
	FallbackResumeRoot string
}

// mailConfig reads the delivery deadline and the thread hop limit out of the user's
// config per send, so editing the file reaches the next send rather than the next
// restart. A config that will not load falls back to the built-in default, since refusing
// to send mail over an unreadable file would be a worse answer than sending it with the
// settings everyone else gets.
type mailConfig func() (*config.Config, error)

func (load mailConfig) GetDeliveryDeadline() (time.Duration, error) {
	cfg, err := load()
	if err != nil {
		return config.Mail{}.GetDeliveryDeadline()
	}

	return cfg.Mail.GetDeliveryDeadline()
}

func (load mailConfig) GetMaxHops() (int, error) {
	cfg, err := load()
	if err != nil {
		return config.Mail{}.GetMaxHops()
	}

	return cfg.Mail.GetMaxHops()
}

// Server is the assembled service, held so its owner can serve it, run its background work,
// and take it back down in that order.
type Server struct {
	// Mux carries the RPC handlers, the MCP endpoint, and the service's own routes such as
	// readiness. Whatever serves it adds nothing but a guard in front.
	Mux *http.ServeMux
	// Jobs is the queue behind the schedules, started and stopped by Start and Stop because a
	// launch in flight has to be waited for rather than cut off.
	Jobs *river.Client[pgx.Tx]
	// Periodic is River's registration bundle, which the schedule loop keeps in step with
	// the table.
	Periodic *jobs.RiverPeriodic
	// Pool is the writable database handle, exposed for the readiness check.
	Pool *store.Pool
	// Delivery is what the queue runs to get an agent to read its mail, exposed so a test
	// can step it the way it steps a loop.
	Delivery *agents.DeliveryService

	// ScheduleReconcile keeps River's periodic registrations in step with the schedules table.
	ScheduleReconcile Loop
	// Reconcile re-observes every agent's pane and writes what it saw.
	Reconcile Loop
	// MailDelivery finds mail nobody has read yet and gets it in front of whoever it is for.
	MailDelivery Loop
	// TranscriptSync folds each agent's new transcript text into the search index.
	TranscriptSync Loop
	// BranchSync re-reads the branch checked out at every live agent's cwd.
	BranchSync Loop

	logger        *slog.Logger
	stopRequested chan struct{}
	background    *backgroundWork
	closers       []func()
}

// loops is every background loop Start has to run, so a loop added above joins this list in
// the same glance.
func (s *Server) loops() []Loop {
	return []Loop{s.ScheduleReconcile, s.Reconcile, s.MailDelivery, s.TranscriptSync, s.BranchSync}
}

// New builds the service. Nothing is listening and no loop is running when it returns; the
// caller decides both.
func New(ctx context.Context, p Params) (*Server, error) {
	// The manager server's shell, since agent panes are created there, and checked ahead
	// of everything else so a service that cannot start an agent says so once rather than
	// leaving each launch to fail in a pane nobody is watching.
	if err := harness.RequirePOSIXShell(p.Tmux.DefaultShell()); err != nil {
		return nil, err
	}

	pool, err := store.Open(ctx, p.Env.DatabaseURL)
	if err != nil {
		return nil, err
	}

	srv := &Server{Pool: pool, logger: p.Logger, stopRequested: make(chan struct{}, 1)}
	srv.closers = append(srv.closers, func() { _ = pool.Close() })

	agentStore := agentstore.New(pool.GetDB())
	workflowStore := workflowstore.New(pool.GetDB())
	scheduleStore := schedulestore.New(pool.GetDB())
	transcriptStore := transcriptstore.New(pool.GetDB())
	mailStore := mailstore.New(pool.GetDB())

	loadConfig := func() (*config.Config, error) { return config.Load(p.ConfigPaths) }

	// Built ahead of the workflow and agent services, which both hand it off as the way to
	// tell a spawning agent its delegate is gone.
	mailService := agents.NewMailService(agents.NewMailServiceParams{
		MailReadWriter: mailStore,
		AgentFinder:    agentStore,
		Config:         mailConfig(loadConfig),
	})
	workflowService := agents.NewWorkflowService(agents.WorkflowParams{
		Workflows: workflowStore,
		Agents:    agentStore,
		Schedules: scheduleStore,
		Tmux:      p.Tmux,
		Tx:        pool,
		MaxAgents: p.MaxAgents,
		Notifier:  mailService,
		Load:      loadConfig,
	})
	scheduleService := agents.NewScheduleService(agents.ScheduleParams{
		Schedules: scheduleStore,
		Workflows: workflowStore,
	})
	// The actions weave ships are written out on every start, so the panel's own keys run
	// this build's scripts rather than an older one's. The directory is named for what puts
	// it there, since installing empties it and the cache directory can be pointed anywhere.
	builtinActionsDir := filepath.Join(p.Env.CacheDir, "weave-builtin-actions")
	if err := actions.InstallBuiltins(builtinActionsDir); err != nil {
		srv.Close()

		return nil, err
	}

	// Shared with the agent service, which fires a workflow's actions around its agents'
	// lives, so a person, a model, and a launch all hand a script the same payload.
	actionService := agents.NewActionService(agents.ActionParams{
		Agents:     agentStore,
		Workflows:  workflowStore,
		Tmux:       p.Tmux,
		Runner:     actions.NewRunner(),
		Load:       loadConfig,
		Addr:       p.Addr,
		Socket:     p.Env.TmuxSocket,
		BuiltinDir: builtinActionsDir,
		Editor:     p.Env.Editor,
		BinPath:    p.HookCommand,
	})
	agentService := agents.NewAgentService(agents.NewAgentServiceParams{
		Tmux:      p.Tmux,
		Agents:    agentStore,
		Workflows: workflowStore,
		Tx:        pool,
		Launcher: agents.NewLauncher(agents.LauncherParams{
			Tmux:        p.Tmux,
			Addr:        p.Addr,
			CacheDir:    p.Env.CacheDir,
			LogPath:     p.Env.LogPath(),
			ContextPath: p.ContextPath,
			HookCommand: p.HookCommand,
		}),
		Harnesses:          p.Harnesses,
		Lifecycle:          actionService,
		UserDefaultHarness: p.DefaultHarness,
		UserDefaultFlags:   p.DefaultFlags,
		MaxAgents:          p.MaxAgents,
		FallbackResumeRoot: p.FallbackResumeRoot,
		Notifier:           mailService,
	})

	// Delivery types at an agent that has stopped and would otherwise never look at its
	// mail. It reads panes through the agent service, which is the one thing that knows
	// whether a pane is still an agent's own.
	srv.Delivery = agents.NewDeliveryService(agents.NewDeliveryServiceParams{
		MailboxReader: mailStore,
		NudgeClaimer:  agentStore,
		PaneReader:    agentService,
		PaneWriter:    p.Tmux,
	})

	// The service is the only thing that runs the queue, so it is the only thing that
	// brings River's tables up. Opening the store did the application's schema.
	if err := db.MigrateRiver(ctx, pool.GetRawPool()); err != nil {
		srv.Close()

		return nil, err
	}

	srv.Jobs, err = jobs.NewClient(jobs.ClientParams{
		Pool:      pool.GetRawPool(),
		Schedules: scheduleStore,
		Agents:    agentService,
		Running:   agentStore,
		Logger:    p.Logger,
	})
	if err != nil {
		srv.Close()

		return nil, err
	}

	// Requests are checked against the constraints declared in the proto before any
	// handler sees them, so a malformed payload is rejected in one place.
	validation, err := interceptors.NewValidation()
	if err != nil {
		srv.Close()

		return nil, err
	}

	options := []connect.HandlerOption{
		connect.WithInterceptors(interceptors.NewLogging(p.Logger), validation),
		interceptors.NewRecovery(),
	}

	srv.Mux = http.NewServeMux()
	srv.handle(HealthPath, newHealthHandler(pool.GetRawPool()))
	srv.handle(StopPath, interceptors.NewLoggingHandler(p.Logger, newStopHandler(srv.stopRequested)))
	srv.handle(agentv1connect.NewAgentServiceHandler(
		rpcagent.NewService(rpcagent.NewServiceParams{
			Agents:      agentStore,
			Transcripts: transcriptStore,
			Mail:        mailService,
			Launcher:    agentService,
		}), options...))
	srv.handle(workflowv1connect.NewWorkflowServiceHandler(
		rpcworkflow.NewService(workflowStore, workflowService), options...))
	srv.handle(schedulev1connect.NewScheduleServiceHandler(
		rpcschedule.NewService(scheduleStore, scheduleService), options...))
	srv.handle(configv1connect.NewConfigServiceHandler(
		rpcconfig.NewService(loadConfig), options...))
	srv.handle(actionv1connect.NewActionServiceHandler(
		rpcaction.NewService(actionService), options...))

	// The MCP endpoint reads through its own read-only handle, opened as a least-privilege
	// role, since what a statement may read is decided by the role and not by the flag.
	readOnly, err := store.OpenReadOnly(ctx, p.Env.ReadOnlyDatabaseURL)
	if err != nil {
		srv.Close()

		return nil, err
	}
	srv.closers = append(srv.closers, readOnly.Close)

	mcpHandler, err := mcp.NewHandler(mcp.NewHandlerParams{DB: readOnly, BaseURL: "http://" + p.Addr})
	if err != nil {
		srv.Close()

		return nil, err
	}
	srv.Mux.Handle("/mcp", interceptors.NewLoggingHandler(p.Logger, mcpHandler))

	srv.Periodic = jobs.NewRiverPeriodic(srv.Jobs.PeriodicJobs())
	srv.ScheduleReconcile = Loop{
		Name:     "schedule reconcile",
		Interval: ScheduleReconcileInterval,
		Pass:     jobs.NewScheduler(scheduleStore, srv.Periodic).Reconcile,
	}
	srv.Reconcile = Loop{
		Name:     "reconcile",
		Interval: ReconcileInterval,
		Pass:     agents.NewReconciler(p.Tmux, p.Harnesses, agentStore).Reconcile,
	}
	srv.MailDelivery = Loop{
		Name:     "mail delivery",
		Interval: MailDeliveryInterval,
		Pass:     srv.Delivery.DeliverAll,
	}
	srv.TranscriptSync = Loop{
		Name:     "transcript sync",
		Interval: TranscriptSyncInterval,
		Pass: agents.NewTranscriptSyncer(agents.TranscriptSyncerParams{
			Agents:      agentStore,
			Transcripts: transcriptStore,
			Harnesses:   p.Harnesses,
		}).Sync,
	}
	srv.BranchSync = Loop{
		Name:     "branch sync",
		Interval: BranchSyncInterval,
		Pass:     agents.NewBranchSyncer(agentStore).Sync,
	}

	return srv, nil
}

// Handler is the mux behind the guard that refuses anything not from this machine.
func (s *Server) Handler() http.Handler {
	return interceptors.NewLocalGuard(s.logger, s.Mux)
}

// Close releases the database handles, in the reverse of the order they were opened.
func (s *Server) Close() {
	for i := len(s.closers) - 1; i >= 0; i-- {
		s.closers[i]()
	}

	s.closers = nil
}

func (s *Server) handle(path string, handler http.Handler) {
	s.Mux.Handle(path, handler)
}
