package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"weave/internal/agents"
	"weave/internal/appconfig"
	"weave/internal/interceptors"
	"weave/internal/jobs"
	"weave/internal/logging"
	"weave/internal/mcp"
	rpcaction "weave/internal/rpc/action"
	rpcagent "weave/internal/rpc/agent"
	rpcconfig "weave/internal/rpc/config"
	rpcschedule "weave/internal/rpc/schedule"
	rpcworkflow "weave/internal/rpc/workflow"
	"weave/internal/store"
	"weave/proto/gen/action/v1/actionv1connect"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/config/v1/configv1connect"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

// reconcileInterval is how often serve re-observes every agent and writes its
// state. It is the only thing that moves an agent's state, so it stays brisk enough
// that starting or finishing work shows within a couple of seconds while the tmux
// captures stay cheap.
const reconcileInterval = 2 * time.Second

// scheduleReconcileInterval is how often serve re-reads the schedules table and
// makes River's periodic jobs match it. River holds those in memory, so this is
// what carries a schedule someone just created into the thing that fires it. It is
// slow because a schedule is a record of future work. A few seconds late to notice
// a new one costs nothing, where the same lag on agent state would show.
const scheduleReconcileInterval = 10 * time.Second

// transcriptSyncInterval is how often serve folds new transcript text into the
// search index. It runs far slower than the state reconcile. A stale search
// corpus is harmless where a stale state count is not, and each pass re-indexes
// potentially large logs, so it stays off the hot path.
const transcriptSyncInterval = 15 * time.Second

func serveCmd(state *app) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the agent store over ConnectRPC/HTTP",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The logger is built before anything that could fail, so a service that
			// cannot start says why in the same shape as one that is running. It writes
			// to stderr rather than opening the log itself, since the process that spawns
			// it already points stderr at appconfig.LogPath and a second handle on the
			// same file would write every line twice.
			logger, err := serveLogger(state.logLevel)
			if err != nil {
				return err
			}
			slog.SetDefault(logger)

			if err := serve(cmd.Context(), state, logger, addr); err != nil {
				logger.LogAttrs(cmd.Context(), slog.LevelError, "the server stopped", logging.Err(err)...)

				// The failure is on the record now, so it is marked as said rather than
				// printed again by main in another shape.
				return fmt.Errorf("%w: %w", ErrLogged, err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8787", "listen address")

	return cmd
}

// serveLogger builds the service's logger at the configured level. A config that would
// not load at all leaves the level empty, which parses as the default rather than
// stopping the service over a log setting.
func serveLogger(level string) (*slog.Logger, error) {
	parsed, err := logging.ParseLevel(level)
	if err != nil {
		return nil, err
	}

	return logging.New(os.Stderr, parsed), nil
}

// serve builds the handlers, starts the background loops, and listens. It is split out of
// the command so every way it can fail lands in one place to be logged, rather than at
// each of the returns in it.
func serve(ctx context.Context, state *app, logger *slog.Logger, addr string) error {
	if err := state.openStore(ctx); err != nil {
		return err
	}

	// Requests are checked against the constraints declared in the proto
	// before any handler sees them, so a malformed payload is rejected in
	// one place rather than at the top of each method.
	validation, err := interceptors.NewValidation()
	if err != nil {
		return err
	}

	// Logging wraps validation rather than the other way round, so a request refused for
	// being malformed is still recorded as one that arrived. Otherwise a caller sending the
	// wrong thing looks the same in the log as one that never called.
	options := connect.WithInterceptors(interceptors.NewLogging(logger), validation)

	mux := http.NewServeMux()
	path, handler := agentv1connect.NewAgentServiceHandler(
		rpcagent.NewService(state.agents, state.transcripts, state.agentService),
		options,
	)
	mux.Handle(path, handler)

	workflowPath, workflowHandler := workflowv1connect.NewWorkflowServiceHandler(
		rpcworkflow.NewService(state.workflows, state.workflowService),
		options,
	)
	mux.Handle(workflowPath, workflowHandler)

	schedulePath, scheduleHandler := schedulev1connect.NewScheduleServiceHandler(
		rpcschedule.NewService(state.schedules, state.scheduleService),
		options,
	)
	mux.Handle(schedulePath, scheduleHandler)

	// The config service reads the file per call, so it takes the path
	// rather than the config this process parsed at startup.
	loadConfig := func() (*appconfig.Config, error) { return appconfig.Load(state.configPath) }

	configPath, configHandler := configv1connect.NewConfigServiceHandler(
		rpcconfig.NewService(loadConfig),
		options,
	)
	mux.Handle(configPath, configHandler)

	// The same service the agent service fires a workflow's actions through, so a
	// script is handed the same payload whoever set it off.
	actionPath, actionHandler := actionv1connect.NewActionServiceHandler(
		rpcaction.NewService(state.actionService),
		options,
	)
	mux.Handle(actionPath, actionHandler)

	// The MCP endpoint reads through its own read-only handle rather than
	// the store's, so nothing reachable from a model can write, whatever
	// the tools do.
	readOnly, err := store.OpenReadOnly(ctx, state.env.DatabaseURL)
	if err != nil {
		return err
	}
	defer readOnly.Close()

	// The RPC-backed tools call back into this same server, so they are
	// pointed at the address it is about to listen on.
	mcpHandler, err := mcp.NewHandler(mcp.NewHandlerParams{DB: readOnly, BaseURL: "http://" + addr})
	if err != nil {
		return err
	}

	// Readiness is answered outside the logging middleware, since whoever is asking is
	// asking on a loop and every answer would be a line.
	mux.Handle(HealthPath, newHealthHandler(state.pool.Raw()))

	// The MCP endpoint is not served through connect, so the interceptors above never see
	// it. It is wrapped here instead, since a surface a model can spawn and delete agents
	// through is the last one that should leave no trace.
	mux.Handle("/mcp", interceptors.NewLoggingHandler(logger, mcpHandler))

	// River runs the schedules. It keeps its own tables in their own schema,
	// one worker that launches an agent, and a reconciler that keeps its
	// periodic jobs matching the schedules table.
	if err := jobs.Migrate(ctx, state.pool.Raw()); err != nil {
		return err
	}

	jobRunner, err := jobs.NewClient(jobs.ClientParams{
		Pool:      state.pool.Raw(),
		Schedules: state.schedules,
		Agents:    state.agentService,
		Running:   state.agents,
	})
	if err != nil {
		return err
	}

	if err := jobRunner.Start(ctx); err != nil {
		return fmt.Errorf("failed to start the job runner: %w", err)
	}
	defer func() { _ = jobRunner.Stop(context.Background()) }()

	periodic := jobs.NewRiverPeriodic(jobRunner.PeriodicJobs())
	go runLoop(ctx, runLoopParams{
		Name:     "schedule reconcile",
		Interval: scheduleReconcileInterval,
		Pass:     jobs.NewScheduler(state.schedules, periodic).Reconcile,
		Logger:   logger,
	})

	// The reconciler runs here rather than in the panel because serve is
	// the always-on process that owns the store. The counts must stay
	// correct even when no panel is open.
	go runLoop(ctx, runLoopParams{
		Name:     "reconcile",
		Interval: reconcileInterval,
		Pass:     agents.NewReconciler(state.tmux, state.harnesses, state.agents).Reconcile,
		Logger:   logger,
	})

	// The transcript syncer runs alongside the reconciler for the same
	// reason. Serve owns the store and is always on, so the search index
	// stays current whether or not a panel is open.
	go runLoop(ctx, runLoopParams{
		Name:     "transcript sync",
		Interval: transcriptSyncInterval,
		Pass: agents.NewTranscriptSyncer(agents.TranscriptSyncerParams{
			Agents:      state.agents,
			Sessions:    state.agents,
			Transcripts: state.transcripts,
			Harnesses:   state.harnesses,
		}).Sync,
		Logger: logger,
	})

	// The socket is claimed here rather than left to ListenAndServe so the line saying the
	// service is up is written only once it is. Serving blocks, so a line written before
	// the call is a claim about something that has not happened yet. A port already taken
	// would put "listening" directly above the error saying it never started.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "the server is listening",
		slog.String(logging.ServerAddress, listener.Addr().String()))

	// The guard wraps the server rather than any one route, so every surface on this mux
	// checks where a request was aimed and one added later cannot be mounted without it.
	server := &http.Server{
		Handler:           interceptors.NewLocalGuard(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return server.Serve(listener)
}

// runLoopParams is what one of serve's background loops needs. Name is carried on every
// line the loop writes, so a failed pass says which of the three it came from.
type runLoopParams struct {
	Name     string
	Interval time.Duration
	Pass     func(context.Context) error
	Logger   *slog.Logger
}

// runLoop re-runs a pass on a fixed interval until the context is cancelled. Every
// loop serve owns is one of these, since all three want the same thing: correct what
// you own, wait, correct it again.
//
// The first pass runs before the first wait, so a restart corrects what the loop owns
// immediately rather than leaving it as the last process left it for an interval.
// Errors are logged and the loop continues, since a single failed pass is corrected by
// the next one.
//
// A loop that keeps failing the same way writes one line and then counts. These run
// against a database, so the usual failure holds for as long as the database is
// unreachable or its schema is behind, and a two-second tick would otherwise write the
// same sentence a thousand times and bury everything else. The repeats stay at debug,
// where somebody chasing a flapping failure can still see each pass.
func runLoop(ctx context.Context, params runLoopParams) {
	ticker := time.NewTicker(params.Interval)
	defer ticker.Stop()

	logger := params.Logger.With(slog.String(logging.Loop, params.Name))

	var failing string
	var repeats int

	for {
		switch err := params.Pass(ctx); {
		case err == nil && failing != "":
			logger.LogAttrs(ctx, slog.LevelInfo, "loop recovered", slog.Int(logging.Repeats, repeats))
			failing, repeats = "", 0
		case err != nil && err.Error() == failing:
			repeats++
			logger.LogAttrs(ctx, slog.LevelDebug, "loop still failing", slog.Int(logging.Repeats, repeats))
		case err != nil:
			failing, repeats = err.Error(), 1
			logger.LogAttrs(ctx, slog.LevelError, "loop pass failed", logging.Err(err)...)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
