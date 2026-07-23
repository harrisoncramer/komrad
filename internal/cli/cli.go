// Package cli builds the claude-agents command tree and wires its dependencies.
package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"claude-agents/config"
	"claude-agents/internal/agents"
	"claude-agents/internal/appconfig"
	"claude-agents/internal/panel"
	"claude-agents/internal/rpc"
	"claude-agents/internal/store"
	"claude-agents/internal/tmux"
	"claude-agents/proto/gen/session/v1/sessionv1connect"

	"github.com/spf13/cobra"
)

type app struct {
	manager  *agents.AgentManager
	sessions *store.Store
	env      *config.Config
	keys     appconfig.Keybindings
}

// NewRootCommand builds the claude-agents command tree. Running it with no
// subcommand opens the panel.
func NewRootCommand() *cobra.Command {
	state := &app{}
	var configPath string

	root := &cobra.Command{
		Use:           "claude-agents [client]",
		Short:         "tmux panel and CLI for background Claude agents",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			env, err := config.Load()
			if err != nil {
				return err
			}

			cfg, err := appconfig.Load(configPath)
			if err != nil {
				return err
			}

			sessions, err := store.Open(env.DBPath)
			if err != nil {
				return err
			}

			state.env = env
			state.keys = cfg.Keybindings
			state.sessions = sessions
			state.manager = agents.NewAgentManager(agents.Params{
				Tmux:        tmux.NewClient(env.TmuxSocket),
				Store:       sessions,
				SelfPath:    selfPath(),
				ClaudeFlags: cfg.ClaudeFlags,
			})
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if err := ensureServer(state.env.Addr, selfPath()); err != nil {
				return err
			}

			sessions := sessionv1connect.NewSessionServiceClient(http.DefaultClient, "http://"+state.env.Addr)
			client := firstOrEmpty(args)

			state.manager.MarkPanelOpen()
			defer state.manager.MarkPanelClosed()

			// The popup is a persistent space. Show the picker, and when the user
			// opens an agent, attach to it in place. When they detach, loop back
			// to the picker with that agent selected. The popup closes only when
			// the picker is quit.
			var focus *store.Agent
			for {
				target, err := panel.Run(state.manager, sessions, client, state.keys, focus)
				if err != nil {
					return err
				}
				if target == nil {
					return nil
				}

				focus = target

				pane := target.Pane
				if !state.manager.SessionAlive(target.Name) {
					restored, err := state.manager.Restore(*target)
					if err != nil {
						return err
					}

					pane = restored
				}

				if err := state.manager.AttachSession(target.Name, pane); err != nil {
					return err
				}
			}
		},
	}

	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config TOML")
	root.AddCommand(
		newCmd(state),
		notifyCmd(state),
		statusCmd(state),
		serveCmd(state),
	)

	return root
}

// reconcileInterval is how often the serve process re-observes every agent and
// writes its state. It is the only thing that moves an agent's state, so it is
// brisk enough that starting or finishing work shows within a couple seconds,
// while the tmux captures stay cheap.
const reconcileInterval = 2 * time.Second

func serveCmd(state *app) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the session store over ConnectRPC/HTTP",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mux := http.NewServeMux()
			path, handler := sessionv1connect.NewSessionServiceHandler(rpc.NewSessionService(state.sessions))
			mux.Handle(path, handler)

			// The reconciler runs here rather than in the panel because serve is
			// the always-on process that owns the store. The counts must stay
			// correct even when no panel is open.
			go runReconcileLoop(cmd.Context(), state.manager.Reconciler(), reconcileInterval)

			server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			return server.ListenAndServe()
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8787", "listen address")

	return cmd
}

// runReconcileLoop reconciles every agent on a fixed interval until the context
// is cancelled. Errors are logged and the loop continues, since a single failed
// pass is corrected by the next one.
func runReconcileLoop(ctx context.Context, rec *agents.Reconciler, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := rec.Reconcile(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "reconcile: %v\n", err)
			}
		}
	}
}

func newCmd(state *app) *cobra.Command {
	var params agents.NewSessionParams

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a background agent session non-interactively",
		RunE: func(_ *cobra.Command, _ []string) error {
			state.manager.NewSession(params)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&params.Title, "title", "", "session title (blank auto-names)")
	flags.StringVar(&params.Dir, "dir", "", "working directory (blank = current)")
	flags.StringVar(&params.Flags, "flags", "", "extra claude flags")
	flags.StringVar(&params.Prompt, "prompt", "", "initial prompt")
	flags.StringVar(&params.Client, "client", "", "invoking tmux client")

	return cmd
}

func notifyCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "notify [message...]",
		Short: "Record this agent as done with a message",
		RunE: func(_ *cobra.Command, args []string) error {
			state.manager.Notify(state.env.MainSession, state.env.TmuxPane, strings.Join(args, " "))
			return nil
		},
	}
}

func statusCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the status-bar aggregate",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Print(state.manager.Status())
			return nil
		},
	}
}

// ensureServer makes sure a session server is reachable at addr, spawning a
// detached `serve` process if not. It returns an error (crashing the caller) if
// the server never becomes reachable.
func ensureServer(addr, self string) error {
	if serverReachable(addr) {
		return nil
	}

	cmd := exec.Command(self, "serve", "--addr", addr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if logFile, err := os.OpenFile(serveLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start session server: %w", err)
	}

	for range 50 {
		if serverReachable(addr) {
			return nil
		}

		time.Sleep(40 * time.Millisecond)
	}

	return fmt.Errorf("session server did not become reachable at %s", addr)
}

func serverReachable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 150*time.Millisecond)
	if err != nil {
		return false
	}

	_ = conn.Close()
	return true
}

func serveLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "claude-agents-serve.log"
	}

	return filepath.Join(home, ".claude", "claude-agents-serve.log")
}

func firstOrEmpty(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}

func selfPath() string {
	path, err := os.Executable()
	if err != nil {
		return "claude-agents"
	}

	return path
}
