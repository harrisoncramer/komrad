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

	"weave/internal/interceptors"
	"weave/internal/logging"
	"weave/internal/server"

	"github.com/spf13/cobra"
)

const httpDrainTimeout = 5 * time.Second

const loopStopTimeout = 2 * time.Second

// River leaves an interrupted job running until its rescuer retries it.
const jobStopTimeout = 10 * time.Second

const closeTimeout = 2 * time.Second

func serveCmd(state *app) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the weave service in the foreground",
		Long: "Run the weave service in the foreground.\n\n" +
			"It opens the database, mounts the Connect handlers and the MCP endpoint on one\n" +
			"address, and runs the loops that read each pane's state, reconcile schedules, and\n" +
			"sync transcripts. The panel and the rest of these commands are clients of it, so\n" +
			"nothing works without it.\n\n" +
			"`weave start` spawns this detached with its output going to the log file, which is\n" +
			"how it usually runs. Run it here to watch that output directly. It stops on an\n" +
			"interrupt, or when `weave stop` asks it to.",
		RunE: func(cmd *cobra.Command, _ []string) error {

			logger, err := serveLogger(state.logLevel)
			if err != nil {
				return err
			}
			slog.SetDefault(logger)

			if state.configErr != nil {
				logger.LogAttrs(cmd.Context(), slog.LevelError,
					"the configuration could not be read, so the defaults are in force",
					logging.Err(state.configErr)...)
			}

			ctx, stopSignals := shutdownSignal(cmd.Context())
			defer stopSignals()

			err = serve(ctx, serveParams{
				App:    state,
				Logger: logger,
				Addr:   resolveServeAddr(addr, state.env.Addr),
			})

			// Cancellation during startup is a requested shutdown, not a startup failure.
			if err != nil && ctx.Err() != nil {
				logger.LogAttrs(context.WithoutCancel(ctx), slog.LevelInfo, "the server stopped before it had started")

				return nil
			}

			if err != nil {
				logger.LogAttrs(cmd.Context(), slog.LevelError, "the server stopped", logging.Err(err)...)

				return fmt.Errorf("%w: %w", ErrLogged, err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "", "listen address, defaulting to WEAVE_ADDR")

	return cmd
}

func resolveServeAddr(flag, configured string) string {
	if flag != "" {
		return flag
	}

	return configured
}

func serveLogger(level string) (*slog.Logger, error) {
	parsed, err := logging.ParseLevel(level)
	if err != nil {
		return nil, err
	}

	return logging.New(os.Stderr, parsed), nil
}

// Releasing the handler after the first signal lets a second signal kill the process.
func shutdownSignal(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	go func() {
		defer logging.LogAndRepanic(ctx)

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

type serveParams struct {
	App    *app
	Logger *slog.Logger
	Addr   string
}

func serve(ctx context.Context, p serveParams) error {
	state, logger, addr := p.App, p.Logger, p.Addr

	built, err := server.New(ctx, server.Params{
		Env:            state.env,
		ConfigPaths:    state.configPaths,
		Logger:         logger,
		Addr:           addr,
		Tmux:           state.tmux,
		Harnesses:      state.harnesses,
		DefaultHarness: state.defaultHarness,
		DefaultFlags:   state.defaultFlags,
		MaxAgents:      state.maxAgents,
		ContextPath:    state.contextPath,
	})
	if err != nil {
		return err
	}
	defer closeWithin(logger, "the database handles", built.Close)

	built.Mux.Handle(HealthPath, newHealthHandler(built.Pool.GetRawPool()))

	stopRequested := make(chan struct{}, 1)
	built.Mux.Handle(StopPath, interceptors.NewLoggingHandler(logger, newStopHandler(stopRequested)))

	// Claim the socket before starting work that would need teardown on failure.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	loops := make([]runLoopParams, 0, len(built.Loops))
	for _, loop := range built.Loops {
		loops = append(loops, runLoopParams{
			Name:     loop.Name,
			Interval: loop.Interval,
			Pass:     loop.Pass,
			Logger:   logger,
		})
	}

	work, err := startBackground(ctx, backgroundParams{Runner: built.Jobs, Loops: loops})
	if err != nil {
		return err
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "the server is listening",
		slog.String(logging.ServerAddress, listener.Addr().String()))

	httpServer := &http.Server{
		Handler:           built.Handler(logger),
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	served := make(chan error, 1)
	go func() {
		defer logging.LogAndRepanic(ctx)

		served <- httpServer.Serve(listener)
	}()

	var failure error
	select {
	case failure = <-served:
	case <-ctx.Done():
		logger.LogAttrs(ctx, slog.LevelInfo, "the server is stopping")
	case <-stopRequested:
		logger.LogAttrs(ctx, slog.LevelInfo, "the server was asked to stop")
	}

	drain, cancelDrain := shutdownDeadline(ctx, httpDrainTimeout)
	defer cancelDrain()

	// Stop accepting requests before tearing down their dependencies.
	if err := httpServer.Shutdown(drain); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "the server would not shut down cleanly", logging.Err(err)...)
	}

	work.stop(context.WithoutCancel(ctx), logger)

	if errors.Is(failure, http.ErrServerClosed) {
		return nil
	}

	return failure
}

func shutdownDeadline(ctx context.Context, within time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), within)
}

type jobRunner interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type backgroundParams struct {
	Runner jobRunner
	Loops  []runLoopParams
}

type backgroundWork struct {
	runner    jobRunner
	stopLoops context.CancelFunc
	loops     *sync.WaitGroup
}

// River's startup context must survive shutdown so Stop can drain active jobs.
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
			defer logging.LogAndRepanic(loopCtx)

			runLoop(loopCtx, params)
		}()
	}

	return &backgroundWork{runner: p.Runner, stopLoops: stopLoops, loops: &loops}, nil
}

// Stop loops before River so they cannot enqueue more work while jobs drain.
func (b *backgroundWork) stop(ctx context.Context, logger *slog.Logger) {
	b.stopLoops()

	if !didFinishWithin(loopStopTimeout, b.loops.Wait) {
		logger.LogAttrs(ctx, slog.LevelWarn, "a background loop did not stop in time")
	}

	deadline, cancel := shutdownDeadline(ctx, jobStopTimeout)
	defer cancel()

	if err := b.runner.Stop(deadline); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "gave up waiting for a job to finish", logging.Err(err)...)
	}
}

func closeWithin(logger *slog.Logger, what string, close func()) {
	if !didFinishWithin(closeTimeout, close) {
		logger.LogAttrs(context.Background(), slog.LevelWarn, "a database handle did not close in time",
			slog.String(logging.Handle, what))
	}
}

func didFinishWithin(within time.Duration, wait func()) bool {
	done := make(chan struct{})
	go func() {
		defer logging.LogAndRepanic(context.Background())

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

type runLoopParams struct {
	Name     string
	Interval time.Duration
	Pass     func(context.Context) error
	Logger   *slog.Logger
}

func runLoop(ctx context.Context, params runLoopParams) {
	ticker := time.NewTicker(params.Interval)
	defer ticker.Stop()

	logger := params.Logger.With(slog.String(logging.Loop, params.Name))

	var failing string
	var repeats int

	for {
		err := runPass(ctx, params.Pass)

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

// Recover inside the loop goroutine so one bad pass does not stop the service.
func runPass(ctx context.Context, pass func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &panicError{value: recovered, stack: string(debug.Stack())}
		}
	}()

	return pass(ctx)
}

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
