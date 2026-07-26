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
	"claude-agents/internal/mcp"
	"claude-agents/internal/rpc"
	"claude-agents/internal/store"
	"claude-agents/internal/tmux"
	"claude-agents/internal/ui"
	"claude-agents/proto/gen/session/v1/sessionv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

// app is what every subcommand is wired from. The services are separate rather
// than one object, so each command holds only the part it drives.
type app struct {
	sessions  *agents.SessionService
	workflows *agents.WorkflowService
	popup     *agents.Popup
	store     *store.Store
	env       *config.Config
	keys      appconfig.Keybindings
	// tmux is shared with the services. The UI needs it directly for the stage,
	// which is its own layout rather than anything the session API owns.
	tmux *tmux.Client
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
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			env, err := config.Load()
			if err != nil {
				return err
			}

			cfg, err := appconfig.Load(configPath)
			if err != nil {
				return err
			}

			sessions, err := store.Open(cmd.Context(), env.DatabaseURL)
			if err != nil {
				return err
			}

			state.env = env
			state.keys = cfg.Keybindings
			state.store = sessions
			state.tmux = tmux.NewClient(env.TmuxSocket)
			state.popup = agents.NewPopup(state.tmux)
			state.workflows = agents.NewWorkflowService(sessions)
			state.sessions = agents.NewSessionService(agents.SessionParams{
				Tmux:        state.tmux,
				Store:       sessions,
				Launcher:    agents.NewLauncher(state.tmux, env.Addr),
				ClaudeFlags: cfg.ClaudeFlags,
			})
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := ensureServer(state.env.Addr, selfPath()); err != nil {
				return err
			}

			sessions := sessionv1connect.NewSessionServiceClient(http.DefaultClient, "http://"+state.env.Addr)

			// Drop this session's status bar so the panel and its stage pane fill
			// the popup without a tmux footer beneath them.
			state.popup.SilenceStatus(agents.ManagerSession)

			// This process runs inside the persistent manager session and never
			// exits on its own. The close and quit keys detach the popup rather
			// than quit, so the panel stays one long-lived program with its state
			// preserved between opens. Run returns only on a real error.
			return ui.Run(ui.Deps{
				Sessions:  state.sessions,
				Workflows: state.workflows,
				Popup:     state.popup,
				Tmux:      state.tmux,
				Client:    sessions,
				Keys:      state.keys,
			})
		},
	}

	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config TOML")
	root.AddCommand(
		openCmd(state),
		newCmd(state),
		statusCmd(state),
		serveCmd(state),
		migrateCmd(),
	)

	return root
}

// migrateCmd brings the database schema up to date and exits.
//
// Opening the store migrates already, so every command does this on the way in
// and a deployment that just starts the binary is up to date. This exists to do
// it on its own, as a step that can run before anything is serving, and to give
// the result an exit code of its own rather than folding a schema failure into
// whatever command happened to be first.
func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "migrate",
		Short:         "apply any pending database migrations",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The store was opened, and so migrated, before this ran.
			cmd.Println("database schema is up to date")
			return nil
		},
	}
}

// reconcileInterval is how often serve re-observes every agent and writes its
// state. It is the only thing that moves an agent's state, so it stays brisk
// enough that starting or finishing work shows within a couple seconds while
// the tmux captures stay cheap.
const reconcileInterval = 2 * time.Second

// transcriptSyncInterval is how often serve folds new transcript text into the
// search index. It runs far slower than the state reconcile. A stale search
// corpus is harmless where a stale state count is not, and each pass re-indexes
// potentially large logs, so it stays off the hot path.
const transcriptSyncInterval = 15 * time.Second

func serveCmd(state *app) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the session store over ConnectRPC/HTTP",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Requests are checked against the constraints declared in the proto
			// before any handler sees them, so a malformed payload is rejected in
			// one place rather than at the top of each method.
			validation, err := rpc.NewValidationInterceptor()
			if err != nil {
				return err
			}

			mux := http.NewServeMux()
			path, handler := sessionv1connect.NewSessionServiceHandler(
				rpc.NewSessionService(state.store, state.sessions),
				connect.WithInterceptors(validation),
			)
			mux.Handle(path, handler)

			// The MCP endpoint reads through its own read-only handle rather than
			// the store's, so nothing reachable from a model can write, whatever
			// the tools do.
			readOnly, err := store.OpenReadOnly(cmd.Context(), state.env.DatabaseURL)
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

			mux.Handle("/mcp", mcpHandler)

			// The reconciler runs here rather than in the panel because serve is
			// the always-on process that owns the store. The counts must stay
			// correct even when no panel is open.
			go runReconcileLoop(cmd.Context(), agents.NewReconciler(state.tmux, state.store), reconcileInterval)

			// The transcript syncer runs alongside the reconciler for the same
			// reason. Serve owns the store and is always on, so the search index
			// stays current whether or not a panel is open.
			go runTranscriptSyncLoop(cmd.Context(), agents.NewTranscriptSyncer(state.store, agents.DefaultTranscriptsDir()), transcriptSyncInterval)

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

// runTranscriptSyncLoop folds new transcript text into the search index on a
// fixed interval until the context is cancelled. Errors are logged and the loop
// continues, since a single failed pass is corrected by the next one.
func runTranscriptSyncLoop(ctx context.Context, syncer *agents.TranscriptSyncer, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := syncer.Sync(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "transcript sync: %v\n", err)
			}
		}
	}
}

// openCmd is the popup entrypoint. It creates the persistent manager session if
// it does not exist and attaches, otherwise attaches to the running one. The
// tmux plugin runs this as the popup's -E command, so it blocks until the client
// detaches and then returns, which closes the popup. TMUX is stripped so tmux
// does not refuse the attach as nested, since the popup itself lives inside the
// user's tmux client.
func openCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "open [client]",
		Short:         "Open the manager popup (create or attach its persistent session)",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, args []string) error {
			// Record the client that opened this popup so panel actions resolve the
			// right directory and client. Mark the popup shown so the binding hides
			// it on the next press. The mark clears when the attach below returns.
			state.popup.SetInvokingClient(firstOrEmpty(args))
			state.popup.MarkOpen()
			defer state.popup.MarkClosed()

			// The panel runs as this session's command, so it inherits the tmux
			// server's environment rather than the popup's. Forward the resolved
			// config with -e so the panel reads the same values this process did,
			// including any shell overrides. -e applies only when the session is
			// first created. On later opens new-session -A just attaches.
			tmuxArgs := []string{
				"-L", state.env.TmuxSocket, "new-session", "-A", "-s", agents.ManagerSession,
				"-e", "CLAUDE_AGENTS_TMUX_SOCKET=" + state.env.TmuxSocket,
				"-e", "DATABASE_URL=" + state.env.DatabaseURL,
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
			created, err := state.sessions.NewSession(params)
			if err != nil {
				return err
			}

			fmt.Println(created.Name)
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

// ensureServer makes sure a session server is reachable at addr, spawning a
// detached serve process if not. It returns an error if the server never
// becomes reachable.
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
