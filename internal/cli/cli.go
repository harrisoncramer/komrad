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
		Use:           "claude-agents",
		Short:         "tmux panel and CLI for background Claude agents",
		Args:          cobra.NoArgs,
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
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := ensureServer(state.env.Addr, selfPath()); err != nil {
				return err
			}

			sessions := sessionv1connect.NewSessionServiceClient(http.DefaultClient, "http://"+state.env.Addr)

			// Drop this session's status bar so the panel and the agents it opens
			// fill the popup without a tmux footer beneath them.
			state.manager.SilenceStatus(agents.ManagerSession)

			// This process runs inside the persistent manager session and never
			// exits on its own: the picker's close and quit keys detach the popup
			// (hiding it) rather than quitting, and opening an agent attaches in
			// place through ExecProcess without leaving the program. So the panel
			// stays a single long-lived program with its state preserved between
			// opens. Run only returns on a real error.
			return panel.Run(state.manager, sessions, state.keys)
		},
	}

	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config TOML")
	root.AddCommand(
		openCmd(state),
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

// openCmd is the popup entrypoint. It creates the persistent manager session if
// it does not exist and attaches to it, otherwise attaches to the running one.
// The tmux plugin runs this as the popup's -E command, so it blocks until the
// client detaches (the popup is hidden or an agent is left) and then returns,
// which closes the popup. TMUX is stripped so tmux does not refuse the attach as
// nested, since the popup itself lives inside the user's tmux client.
func openCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "open [client]",
		Short:         "Open the manager popup (create or attach its persistent session)",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, args []string) error {
			// Record the client that opened this popup so panel actions resolve the
			// right directory and client, and mark the popup shown so the binding
			// hides it on the next press. The mark is cleared when the attach below
			// returns (the popup was hidden or an agent left).
			state.manager.SetInvokingClient(firstOrEmpty(args))
			state.manager.MarkOpen()
			defer state.manager.MarkClosed()

			// The panel runs as this session's command, so it inherits the tmux
			// server's environment rather than the popup's. Forward the resolved
			// config with -e so the panel reads the same values this process did,
			// including any shell overrides. -e applies only when the session is
			// first created; on later opens new-session -A just attaches.
			tmuxArgs := []string{
				"-L", state.env.TmuxSocket, "new-session", "-A", "-s", agents.ManagerSession,
				"-e", "CLAUDE_AGENTS_TMUX_SOCKET=" + state.env.TmuxSocket,
				"-e", "CLAUDE_AGENTS_DB=" + state.env.DBPath,
				"-e", "CLAUDE_AGENTS_ADDR=" + state.env.Addr,
				"--", selfPath(),
			}

			cmd := exec.Command("tmux", tmuxArgs...)
			cmd.Env = envWithoutTmux()
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to open manager popup: %w", err)
			}

			return nil
		},
	}
}

// envWithoutTmux returns the current environment with TMUX removed, so a tmux
// command run from inside a tmux client is not refused as a nested session.
func envWithoutTmux() []string {
	full := os.Environ()
	filtered := make([]string, 0, len(full))
	for _, entry := range full {
		if !strings.HasPrefix(entry, "TMUX=") {
			filtered = append(filtered, entry)
		}
	}

	return filtered
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
