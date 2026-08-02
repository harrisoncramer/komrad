// Package server assembles the service: the stores, the domain services over them, the
// handlers those are served through, and the background loops that correct what the panel
// shows. It holds the wiring and nothing about how the process is started, so the same
// service a user runs can be stood up inside a test.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"weave/config"
	"weave/db"
	"weave/internal/actions"
	"weave/internal/agents"
	"weave/internal/appconfig"
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

// TranscriptSyncInterval is how often the service folds new transcript text into the
// search index. A stale search corpus is harmless where a stale state count is not, and
// each pass re-indexes potentially large logs, so it stays off the hot path.
const TranscriptSyncInterval = 15 * time.Second

// Params is everything the service is built from. Every one of them is gathered before
// this is reached, so nothing here reads a file, an environment, or a flag.
type Params struct {
	// Env is the process configuration the connection strings and paths come from.
	Env *config.Config
	// ConfigPath is the user's config file, empty for the default location. The path is
	// kept rather than the config parsed from it, since the config service and the action
	// service both read the file per request.
	ConfigPath string
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
	// FallbackResumeRoot is where an agent runs when the directory it was created in cannot be
	// brought back. Empty takes the default beside the user's own agent state.
	FallbackResumeRoot string
}

// Loop is one pass the service re-runs on an interval. How it is run, logged, and stopped
// belongs to whoever is running the service rather than here.
type Loop struct {
	Name     string
	Interval time.Duration
	Pass     func(context.Context) error
}

// Server is the assembled service, held so its owner can serve it, run its loops, and take
// it back down in that order.
type Server struct {
	// Mux carries the RPC handlers and the MCP endpoint. Whatever is serving it adds the
	// routes that belong to the process rather than the service, such as readiness.
	Mux *http.ServeMux
	// Loops is the background work, in no particular order.
	Loops []Loop
	// Jobs is the queue behind the schedules, started and stopped by the owner because a
	// launch in flight has to be waited for rather than cut off.
	Jobs *river.Client[pgx.Tx]
	// Periodic is River's registration bundle, which the schedule loop keeps in step with
	// the table.
	Periodic *jobs.RiverPeriodic
	// Pool is the writable database handle, exposed for the readiness check.
	Pool *store.Pool

	closers []func()
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

	srv := &Server{Pool: pool}
	srv.closers = append(srv.closers, func() { _ = pool.Close() })

	agentStore := agentstore.New(pool.GetDB())
	workflowStore := workflowstore.New(pool.GetDB())
	scheduleStore := schedulestore.New(pool.GetDB())
	transcriptStore := transcriptstore.New(pool.GetDB())

	loadConfig := func() (*appconfig.Config, error) { return appconfig.Load(p.ConfigPath) }

	workflowService := agents.NewWorkflowService(agents.WorkflowParams{
		Workflows: workflowStore,
		Schedules: scheduleStore,
		Tx:        pool,
		MaxAgents: p.MaxAgents,
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
		}),
		Harnesses:          p.Harnesses,
		Lifecycle:          actionService,
		UserDefaultHarness: p.DefaultHarness,
		UserDefaultFlags:   p.DefaultFlags,
		MaxAgents:          p.MaxAgents,
		FallbackResumeRoot: p.FallbackResumeRoot,
	})

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
	srv.handle(agentv1connect.NewAgentServiceHandler(
		rpcagent.NewService(agentStore, transcriptStore, agentService), options...))
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

	srv.Periodic = jobs.NewRiverPeriodic(srv.Jobs.PeriodicJobs())
	srv.Loops = []Loop{
		{
			Name:     "schedule reconcile",
			Interval: ScheduleReconcileInterval,
			Pass:     jobs.NewScheduler(scheduleStore, srv.Periodic).Reconcile,
		},
		{
			Name:     "reconcile",
			Interval: ReconcileInterval,
			Pass:     agents.NewReconciler(p.Tmux, p.Harnesses, agentStore).Reconcile,
		},
		{
			Name:     "transcript sync",
			Interval: TranscriptSyncInterval,
			Pass: agents.NewTranscriptSyncer(agents.TranscriptSyncerParams{
				Agents:      agentStore,
				Sessions:    agentStore,
				Transcripts: transcriptStore,
				Harnesses:   p.Harnesses,
			}).Sync,
		},
	}

	return srv, nil
}

// Handler is the mux behind the guard that refuses anything not from this machine. It is
// taken once the owner has added its own routes, since the guard has to sit outside those
// too.
func (s *Server) Handler(logger *slog.Logger) http.Handler {
	return interceptors.NewLocalGuard(logger, s.Mux)
}

// LoopNamed returns one loop's pass by name, for a caller that runs the loops itself
// rather than on their intervals. It panics on a name no loop holds, since that is a typo
// in the caller rather than anything that happens at runtime.
func (s *Server) LoopNamed(name string) func(context.Context) error {
	for _, loop := range s.Loops {
		if loop.Name == name {
			return loop.Pass
		}
	}

	panic(fmt.Sprintf("no loop named %q", name))
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
