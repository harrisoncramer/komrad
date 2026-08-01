package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
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

// reconcileInterval is how often serve re-observes every agent and writes its state. It
// stays brisk enough that starting or finishing work shows within a couple of seconds,
// while the tmux captures stay cheap.
const reconcileInterval = 2 * time.Second

// scheduleReconcileInterval is how often serve re-reads the schedules table and makes
// River's periodic jobs match it, since River holds those in memory. It is slow because a
// schedule is a record of future work, where the same lag on agent state would show.
const scheduleReconcileInterval = 10 * time.Second

// transcriptSyncInterval is how often serve folds new transcript text into the search
// index. A stale search corpus is harmless where a stale state count is not, and each pass
// re-indexes potentially large logs, so it stays off the hot path.
const transcriptSyncInterval = 15 * time.Second

func serveCmd(state *app) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the agent store over ConnectRPC/HTTP",
		RunE: func(cmd *cobra.Command, _ []string) error {

			// The logger is built before anything that could fail, so a service that
			// cannot start says why in the same shape as one that is running.
			logger, err := serveLogger(state.logLevel)
			if err != nil {
				return err
			}
			slog.SetDefault(logger)

			if err := serve(cmd.Context(), state, logger, addr); err != nil {
				logger.LogAttrs(cmd.Context(), slog.LevelError, "the server stopped", logging.Err(err)...)

				// The failure is on the record now, so it is marked as said rather than
				// printed again by main.
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

// serve builds the handlers, starts the background loops, and listens.
func serve(ctx context.Context, state *app, logger *slog.Logger, addr string) error {
	if err := state.openStore(ctx); err != nil {
		return err
	}

	// Requests are checked against the constraints declared in the proto before any
	// handler sees them, so a malformed payload is rejected in one place.
	validation, err := interceptors.NewValidation()
	if err != nil {
		return err
	}

	// Connect options are logging and validation through CEL expressions
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

	// The config service reads the file per call, so it takes the path rather than the
	// config this process parsed at startup.
	loadConfig := func() (*appconfig.Config, error) { return appconfig.Load(state.configPath) }

	configPath, configHandler := configv1connect.NewConfigServiceHandler(
		rpcconfig.NewService(loadConfig),
		options,
	)
	mux.Handle(configPath, configHandler)

	actionPath, actionHandler := actionv1connect.NewActionServiceHandler(
		rpcaction.NewService(state.actionService),
		options,
	)
	mux.Handle(actionPath, actionHandler)

	// The MCP endpoint reads through its own read-only handle, opened as a least-privilege
	// role, since what a statement may read is decided by the role and not by the flag.
	readOnly, err := store.OpenReadOnly(ctx, state.env.ReadOnlyDatabaseURL)
	if err != nil {
		return err
	}
	defer readOnly.Close()

	mcpHandler, err := mcp.NewHandler(mcp.NewHandlerParams{
		DB:      readOnly,
		BaseURL: "http://" + addr,
	})
	if err != nil {
		return err
	}

	mux.Handle("/mcp", interceptors.NewLoggingHandler(logger, mcpHandler))

	mux.Handle(HealthPath, newHealthHandler(state.pool.Raw()))

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

	go runLoop(ctx, runLoopParams{
		Name:     "reconcile",
		Interval: reconcileInterval,
		Pass:     agents.NewReconciler(state.tmux, state.harnesses, state.agents).Reconcile,
		Logger:   logger,
	})

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

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "the server is listening",
		slog.String(logging.ServerAddress, listener.Addr().String()))

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

// runLoop re-runs a pass on a fixed interval until the context is cancelled. Every loop
// serve owns is one of these: correct what you own, wait, correct it again.
//
// A pass that panics is a failing pass too, for the reasons on runPass, so it folds and
// counts the same way rather than ending the process.
func runLoop(ctx context.Context, params runLoopParams) {
	ticker := time.NewTicker(params.Interval)
	defer ticker.Stop()

	logger := params.Logger.With(slog.String(logging.Loop, params.Name))

	var failing string
	var repeats int

	for {
		switch err := runPass(ctx, params.Pass); {
		case err == nil && failing != "":
			logger.LogAttrs(ctx, slog.LevelInfo, "loop recovered", slog.Int(logging.Repeats, repeats))
			failing, repeats = "", 0
		case err != nil && err.Error() == failing:
			repeats++
			logger.LogAttrs(ctx, slog.LevelDebug, "loop still failing", slog.Int(logging.Repeats, repeats))
		case err != nil:
			failing, repeats = err.Error(), 1
			logger.LogAttrs(ctx, slog.LevelError, "loop pass failed", failureAttrs(err)...)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runPass runs one pass, turning a panic into a failed pass rather than the end of the
// process. Go ends the program on a panic in any goroutine that does not catch one, and
// every loop here is one, so without this a single bad pass takes the other two loops, the
// RPC surface, and the MCP endpoint with it.
func runPass(ctx context.Context, pass func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &panicError{value: recovered, stack: string(debug.Stack())}
		}
	}()

	return pass(ctx)
}

// panicError is a panic a pass raised, carried as an error so it is reported through the
// same path a returned failure is.
type panicError struct {
	value any
	stack string
}

func (e *panicError) Error() string {
	return fmt.Sprintf("the pass panicked: %v", e.value)
}

func failureAttrs(err error) []slog.Attr {
	attrs := logging.Err(err)

	var panicked *panicError
	if errors.As(err, &panicked) {
		attrs = append(attrs, slog.String(logging.StackTrace, panicked.stack))
	}

	return attrs
}
