package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
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

const ReconcileInterval = 2 * time.Second

const ScheduleReconcileInterval = 10 * time.Second

const MailDeliveryInterval = 2 * time.Second

const TranscriptSyncInterval = 15 * time.Second

const BranchSyncInterval = 15 * time.Second

const WorktreeSyncInterval = 15 * time.Second

type Params struct {
	Env *config.Environment

	ConfigPaths []string

	Logger *slog.Logger

	Addr string

	Tmux *tmux.Servers

	Harnesses *harness.Registry

	DefaultHarness string

	DefaultFlags map[string]string

	MaxAgents int

	ContextPath string

	HookCommand string

	FallbackResumeRoot string
}

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

type Server struct {
	Mux *http.ServeMux

	Jobs *river.Client[pgx.Tx]

	Periodic *jobs.RiverPeriodic

	Pool *store.Pool

	Delivery *agents.DeliveryService

	ScheduleReconcile Loop

	Reconcile Loop

	MailDelivery Loop

	TranscriptSync Loop

	BranchSync Loop

	WorktreeSync Loop

	logger        *slog.Logger
	stopRequested chan struct{}
	background    *backgroundWork
	closers       []func()
}

func (s *Server) loops() []Loop {
	return []Loop{s.ScheduleReconcile, s.Reconcile, s.MailDelivery, s.TranscriptSync, s.BranchSync, s.WorktreeSync}
}

// New builds the service. Nothing is listening and no loop is running when it returns; the
// caller decides both.
func New(ctx context.Context, p Params) (*Server, error) {
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
	})
	scheduleService := agents.NewScheduleService(agents.ScheduleParams{
		Schedules: scheduleStore,
		Workflows: workflowStore,
	})

	builtinActionsDir := filepath.Join(p.Env.CacheDir, "weave-builtin-actions")
	if err := actions.InstallBuiltins(builtinActionsDir); err != nil {
		srv.Close()

		return nil, err
	}

	actionLogsDir := p.Env.ActionLogsDir()
	if err := os.MkdirAll(actionLogsDir, 0o755); err != nil {
		srv.Close()

		return nil, fmt.Errorf("failed to create %s: %w", actionLogsDir, err)
	}

	actionService := agents.NewActionService(agents.ActionParams{
		Agents:     agentStore,
		Workflows:  workflowStore,
		Tmux:       p.Tmux,
		Runner:     actions.NewRunner(),
		Load:       loadConfig,
		Addr:       p.Addr,
		Socket:     p.Env.TmuxSocket,
		BuiltinDir: builtinActionsDir,
		LogsDir:    actionLogsDir,
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
		LogsDir:            actionLogsDir,
	})

	srv.Delivery = agents.NewDeliveryService(agents.NewDeliveryServiceParams{
		MailboxReader: mailStore,
		NudgeClaimer:  agentStore,
		PaneReader:    agentService,
		PaneTyper:     p.Tmux,
	})

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
		Pass:     agents.NewReconciler(p.Tmux, p.Harnesses, agentStore).WithPRFinder(agents.NewGHPRFinder()).Reconcile,
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
	srv.WorktreeSync = Loop{
		Name:     "worktree sync",
		Interval: WorktreeSyncInterval,
		Pass:     agents.NewWorktreeSyncer(agentStore).Sync,
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
