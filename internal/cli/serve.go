package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
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

// The teardown gives each stage its own budget rather than sharing one, so a stage that
// runs long cannot spend what the next one needs.

// httpDrainTimeout bounds the wait for the requests already in flight. It is the shortest
// of the three because whoever was asking will ask again.
const httpDrainTimeout = 5 * time.Second

// loopStopTimeout bounds the wait for a background loop to finish the pass it is in.
// Abandoning a pass costs nothing, since the next start redoes it, but a moment's wait
// keeps one from writing to a database that is closing under it. It is bounded because a
// pass talks to tmux, and a tmux server that is not answering would otherwise hold the
// process open for good.
const loopStopTimeout = 2 * time.Second

// jobStopTimeout is how long a launch in flight is given to finish, and it is the one
// that matters. A job abandoned here stays marked running until River's rescuer comes
// back for it over an hour later.
const jobStopTimeout = 10 * time.Second

// closeTimeout bounds the wait for the database handles. A pool closes by waiting for its
// connections to come back, so a query still running would otherwise hold the process
// open past every deadline above it.
const closeTimeout = 2 * time.Second

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

			ctx, stopSignals := shutdownSignal(cmd.Context())
			defer stopSignals()

			err = serve(ctx, serveParams{App: state, Logger: logger, Addr: addr})

			// A signal landing partway through startup fails whichever step was reaching
			// for the database at the time. That is the shutdown arriving early rather
			// than something going wrong, and reporting it as a failure would have
			// whoever spawned this process read a reload as a crash.
			if err != nil && ctx.Err() != nil {
				logger.LogAttrs(context.WithoutCancel(ctx), slog.LevelInfo, "the server stopped before it had started")

				return nil
			}

			if err != nil {
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

// shutdownSignal returns a context cancelled by the first interrupt or termination
// signal. The handler is released as that signal lands, so a second one gets the default
// disposition and kills the process outright.
func shutdownSignal(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-signals:
			signal.Stop(signals)
			cancel()
		case <-ctx.Done():
		}
	}()

	return ctx, func() {
		signal.Stop(signals)
		cancel()
	}
}

// serveParams is what serve needs to build the service and run it.
type serveParams struct {
	App    *app
	Logger *slog.Logger
	Addr   string
}

// serve builds the handlers, starts the background loops, and listens until the context
// is cancelled or serving fails. A shutdown that was asked for returns nil, since
// something spawned this process and reads its status.
func serve(ctx context.Context, p serveParams) error {
	state, logger, addr := p.App, p.Logger, p.Addr

	if err := state.openStore(ctx); err != nil {
		return err
	}
	defer closeWithin(logger, "the store", func() { _ = state.pool.Close() })

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
	defer closeWithin(logger, "the read-only store", readOnly.Close)

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

	// The socket is claimed before the background work starts, so a port already taken is
	// reported without anything having to be stopped again on the way out.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	periodic := jobs.NewRiverPeriodic(jobRunner.PeriodicJobs())

	work, err := startBackground(ctx, backgroundParams{
		Runner: jobRunner,
		Loops: []runLoopParams{
			{
				Name:     "schedule reconcile",
				Interval: scheduleReconcileInterval,
				Pass:     jobs.NewScheduler(state.schedules, periodic).Reconcile,
				Logger:   logger,
			},
			{
				Name:     "reconcile",
				Interval: reconcileInterval,
				Pass:     agents.NewReconciler(state.tmux, state.harnesses, state.agents).Reconcile,
				Logger:   logger,
			},
			{
				Name:     "transcript sync",
				Interval: transcriptSyncInterval,
				Pass: agents.NewTranscriptSyncer(agents.TranscriptSyncerParams{
					Agents:      state.agents,
					Sessions:    state.agents,
					Transcripts: state.transcripts,
					Harnesses:   state.harnesses,
				}).Sync,
				Logger: logger,
			},
		},
	})
	if err != nil {
		return err
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "the server is listening",
		slog.String(logging.ServerAddress, listener.Addr().String()))

	server := &http.Server{
		Handler:           interceptors.NewLocalGuard(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	var failure error
	select {
	case failure = <-served:
	case <-ctx.Done():
		logger.LogAttrs(ctx, slog.LevelInfo, "the server is stopping")
	}

	drain, cancelDrain := shutdownDeadline(ctx, httpDrainTimeout)
	defer cancelDrain()

	// The listener goes first, so nothing new arrives against a dependency that is
	// already being torn down.
	if err := server.Shutdown(drain); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "the server would not shut down cleanly", logging.Err(err)...)
	}

	work.stop(context.WithoutCancel(ctx), logger)

	if errors.Is(failure, http.ErrServerClosed) {
		return nil
	}

	return failure
}

// shutdownDeadline gives one stage of the teardown its budget, on a context the signal
// that started the teardown does not reach.
func shutdownDeadline(ctx context.Context, within time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), within)
}

// jobRunner is what serve needs of River, which is only the two ends of its life.
type jobRunner interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// backgroundParams is the work that runs behind the listener rather than in answer to a
// request.
type backgroundParams struct {
	Runner jobRunner
	Loops  []runLoopParams
}

// backgroundWork is that work once started, held so serve can stop it in order.
type backgroundWork struct {
	runner    jobRunner
	stopLoops context.CancelFunc
	loops     *sync.WaitGroup
}

// startBackground starts River and the loops on a context the signal deliberately does
// not reach. Cancelling the context River was started on is a hard stop, which would cut
// a launch off mid-flight rather than waiting for it to finish.
func startBackground(ctx context.Context, p backgroundParams) (*backgroundWork, error) {
	uncancelled := context.WithoutCancel(ctx)

	if err := p.Runner.Start(uncancelled); err != nil {
		return nil, fmt.Errorf("failed to start the job runner: %w", err)
	}

	loopCtx, stopLoops := context.WithCancel(uncancelled)

	var loops sync.WaitGroup
	for _, params := range p.Loops {
		loops.Add(1)

		go func() {
			defer loops.Done()
			runLoop(loopCtx, params)
		}()
	}

	return &backgroundWork{runner: p.Runner, stopLoops: stopLoops, loops: &loops}, nil
}

// stop ends the loops and then waits for River, which waits in turn for a launch in
// flight. A job still running at the deadline is left where it is rather than cancelled,
// since a launch cut off partway through leaves a pane no row names.
func (b *backgroundWork) stop(ctx context.Context, logger *slog.Logger) {
	b.stopLoops()

	if !awaitWithin(loopStopTimeout, b.loops.Wait) {
		logger.LogAttrs(ctx, slog.LevelWarn, "a background loop did not stop in time")
	}

	deadline, cancel := shutdownDeadline(ctx, jobStopTimeout)
	defer cancel()

	if err := b.runner.Stop(deadline); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "gave up waiting for a job to finish", logging.Err(err)...)
	}
}

// closeWithin closes a database handle without letting a query still running hold the
// process open. What did not close goes when the process does.
func closeWithin(logger *slog.Logger, what string, close func()) {
	if !awaitWithin(closeTimeout, close) {
		logger.LogAttrs(context.Background(), slog.LevelWarn, "a database handle did not close in time",
			slog.String(logging.Handle, what))
	}
}

// awaitWithin runs wait and reports whether it finished inside the budget. Whatever is
// being waited on carries on in the background if it does not, since the point here is
// only that the teardown moves on rather than stalling on it.
func awaitWithin(within time.Duration, wait func()) bool {
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()

	timer := time.NewTimer(within)
	defer timer.Stop()

	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
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
		err := runPass(ctx, params.Pass)

		// A pass cut short by the shutdown is not a failure. Every store call it makes
		// takes this context, so without this a clean stop writes one last error naming
		// every agent the pass had left to do.
		if ctx.Err() != nil {
			return
		}

		switch {
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
