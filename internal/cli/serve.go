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
	serverPkg "weave/internal/server"
	"weave/lib/xsync"

	"github.com/spf13/cobra"
)

const httpDrainTimeout = 5 * time.Second

const closeTimeout = 2 * time.Second

func serveCmd(state *app) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the weave service in the foreground",
		Long: `Run the weave service in the foreground.

It opens the database, mounts the Connect handlers and the MCP endpoint on one
address, and runs the loops that read each pane's state, reconcile schedules, and
sync transcripts. The panel and the rest of these commands are clients of it, so
nothing works without it.

` + "`weave start`" + ` spawns this detached with its output going to the log file, which is
how it usually runs. Run it here to watch that output directly. It stops on an
interrupt, or when ` + "`weave stop`" + ` asks it to.`,
		Args: cobra.NoArgs,
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

	// `weave start` points this process's stderr at the log file the panel also appends to.
	return logging.New(os.Stderr, parsed).With(slog.String(logging.ProcessName, logging.ProcessServe)), nil
}

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

func getHookBinaryPath(ctx context.Context) string {
	path, err := os.Executable()
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn,
			"failed to locate this binary, so agents launch with no hooks and nothing delivers their mail",
			logging.Err(err)...)

		return ""
	}

	return path
}

func serve(ctx context.Context, params serveParams) error {
	state, logger, addr := params.App, params.Logger, params.Addr

	server, err := serverPkg.New(ctx, serverPkg.Params{
		Env:            state.env,
		ConfigPaths:    state.configPaths,
		Logger:         logger,
		Addr:           addr,
		Tmux:           state.tmux,
		Harnesses:      state.harnesses,
		DefaultHarness: state.defaultHarness,
		DefaultFlags:   state.defaultFlags,
		MaxAgents:      state.maxAgents,
		MaxChildren:    state.maxChildren,
		ContextPath:    state.contextPath,
		HookCommand:    getHookBinaryPath(ctx),
	})
	if err != nil {
		return err
	}
	defer closeWithin(logger, "the database handles", server.Close)

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	if err := server.Start(ctx); err != nil {
		return err
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "the server is listening",
		slog.String(logging.ServerAddress, listener.Addr().String()))

	httpServer := &http.Server{
		Handler:           server.Handler(),
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
	case <-server.StopRequested():
		logger.LogAttrs(ctx, slog.LevelInfo, "the server was asked to stop")
	}

	drain, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), httpDrainTimeout)
	defer cancelDrain()

	if err := httpServer.Shutdown(drain); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "the server would not shut down cleanly", logging.Err(err)...)
	}

	server.Stop(context.WithoutCancel(ctx))

	if errors.Is(failure, http.ErrServerClosed) {
		return nil
	}

	return failure
}

func closeWithin(logger *slog.Logger, what string, close func()) {
	finished := xsync.WithinTimeout(closeTimeout, func() {
		defer logging.LogAndRepanic(context.Background())

		close()
	})
	if !finished {
		logger.LogAttrs(context.Background(), slog.LevelWarn, "a database handle did not close in time",
			slog.String(logging.Handle, what))
	}
}
