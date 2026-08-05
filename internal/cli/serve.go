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
	"syscall"
	"time"

	"weave/internal/logging"
	"weave/internal/server"

	"github.com/spf13/cobra"
)

const httpDrainTimeout = 5 * time.Second

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

// hookCommand is what an agent CLI's hooks run to reach this panel, which is this binary
// itself. A build that cannot say where it lives launches agents with no hooks rather than
// registering a command a hook would fail to run, so mail waits to be read instead of
// being delivered.
func hookCommand(ctx context.Context) string {
	path, err := os.Executable()
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn,
			"failed to locate this binary, so agents launch with no hooks and nothing delivers their mail",
			logging.Err(err)...)

		return ""
	}

	return path
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
		HookCommand:    hookCommand(ctx),
	})
	if err != nil {
		return err
	}
	defer closeWithin(logger, "the database handles", built.Close)

	// Claim the socket before starting work that would need teardown on failure.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	if err := built.Start(ctx); err != nil {
		return err
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "the server is listening",
		slog.String(logging.ServerAddress, listener.Addr().String()))

	httpServer := &http.Server{
		Handler:           built.Handler(),
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
	case <-built.StopRequested():
		logger.LogAttrs(ctx, slog.LevelInfo, "the server was asked to stop")
	}

	drain, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), httpDrainTimeout)
	defer cancelDrain()

	// Stop accepting requests before tearing down their dependencies.
	if err := httpServer.Shutdown(drain); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "the server would not shut down cleanly", logging.Err(err)...)
	}

	built.Stop(context.WithoutCancel(ctx))

	if errors.Is(failure, http.ErrServerClosed) {
		return nil
	}

	return failure
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
