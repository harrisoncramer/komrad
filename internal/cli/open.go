package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"komrad/internal/agents"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/tmux"
	"komrad/lib/xhttp"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	"github.com/spf13/cobra"
)

func openCmd(state *app) *cobra.Command {
	var (
		detach   bool
		headless bool
	)

	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the panel in this terminal, creating or attaching its persistent session",
		Long: `Open the panel in this terminal.

It creates the manager session if it is not there and attaches either way, so the
panel keeps its place between opens rather than starting over. The manager session
lives on komrad's own tmux server, so it does not show up in ` + "`tmux ls`" + `.

It opens this machine's panel, or the engine --engine names.

The terminal becomes the tmux client, so a detach returns to the shell. Run from
inside tmux it nests the panel in the current pane.

With --detach it creates the session and returns, which is what a script wants when
it needs somewhere for agents to be put back into.

With --headless it creates that session holding nothing but a placeholder, for an
engine whose panel runs on somebody else's machine and which would otherwise swap
an agent's pane into a panel nobody is looking at.`,
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !detach && !headless {
				if err := refuseNesting(state); err != nil {
					return err
				}
			}

			return openHere(cmd.Context(), state, detach, headless)
		},
	}

	cmd.Flags().BoolVar(&detach, "detach", false, "create the manager session if it is not there, then return without attaching")
	cmd.Flags().BoolVar(&headless, "headless", false, "create the manager session with no panel in it, for an engine driven from another machine")

	return cmd
}

// openHere puts this engine's panel up and turns this process into the client attached to it.
func openHere(ctx context.Context, state *app, detach, headless bool) error {
	settingUp := !headless && prepareAutoSetup(ctx, state)

	if err := ensureManagerSession(ctx, state, detach, headless); err != nil {
		return err
	}

	if settingUp {
		setUpWorkflowHere(ctx, state)
	}

	state.tmux.BlankBorders()
	state.tmux.InsetPanelWindow(agents.ManagerSession)

	if detach || headless {
		return nil
	}

	// Only the attaching path says which window is current.
	state.tmux.SelectWindow(state.tmux.GetPanelPane())

	socket := tmux.SocketFromEnv(state.env.Tmux)
	state.tmux.RecordHostSocket(socket)
	state.tmux.RecordHostPath(hostPath(socket))

	return attachManagerSession(state)
}

// attachManagerSession replaces this process with the tmux client.
func attachManagerSession(state *app) error {
	return state.tmux.ExecAttachSession(agents.ManagerSession)
}

func isManagerSessionRunning(ctx context.Context, state *app) bool {
	return state.tmux.HasSession(ctx, agents.ManagerSession)
}

// isPanelRunning reports whether a panel is drawing in the manager session.
func isPanelRunning(ctx context.Context, state *app) bool {
	if !isManagerSessionRunning(ctx, state) {
		return false
	}

	return state.tmux.IsPaneAlive(state.tmux.GetPanelPane())
}

// ensureManagerSession puts a panel up, making the session when there is none and a window in
// the session it already has when only the panel has gone.
func ensureManagerSession(ctx context.Context, state *app, detached, headless bool) error {
	if !isManagerSessionRunning(ctx, state) {
		if err := runTmux(ctx, state, "create the manager session",
			buildManagerSessionArgs(state, state.selfPath(), headless)); err != nil {
			return err
		}

		if headless {
			silenceManagerPrefix(state)
		}

		return nil
	}

	if headless || isPanelRunning(ctx, state) {
		return nil
	}

	return runTmux(ctx, state, "open a panel window in the manager session",
		buildPanelWindowArgs(state, state.selfPath(), detached))
}

// silenceManagerPrefix leaves the engine's own server answering to no prefix.
func silenceManagerPrefix(state *app) {
	state.tmux.SetGlobal("prefix", "None")
	state.tmux.SetGlobal("prefix2", "None")
}

func runTmux(ctx context.Context, state *app, summary string, args []string) error {
	if err := state.tmux.RunToStderr(ctx, args...); err != nil {
		return fmt.Errorf("failed to %s: %w", summary, err)
	}

	return nil
}

func buildManagerSessionArgs(state *app, self string, headless bool) []string {
	command := panelCommandArgs(state, self)
	if headless {
		command = holderCommandArgs(state)
	}

	return append([]string{"new-session", "-d", "-s", agents.ManagerSession}, command...)
}

func holderCommandArgs(state *app) []string {
	return append(sessionEnvArgs(state), "--", "sh", "-c", "while :; do sleep 86400; done")
}

// buildPanelWindowArgs opens the panel in a session that is already there, detached when the
// caller is not about to attach.
func buildPanelWindowArgs(state *app, self string, detached bool) []string {
	args := []string{"new-window"}
	if detached {
		args = append(args, "-d")
	}

	return append(append(args, "-t", agents.ManagerSession+":"), panelCommandArgs(state, self)...)
}

func sessionEnvArgs(state *app) []string {
	return []string{
		"-e", "KOMRAD_TMUX_SOCKET=" + state.env.TmuxSocket,
		"-e", "KOMRAD_DATABASE_URL=" + state.env.DatabaseURL,
		"-e", "KOMRAD_READONLY_DATABASE_URL=" + state.env.ReadOnlyDatabaseURL,
		"-e", "KOMRAD_ADDR=" + state.env.Addr,
		"-e", "EDITOR=" + state.env.Editor,
		"-e", "KOMRAD_CACHE_DIR=" + state.env.CacheDir,
		"-e", "KOMRAD_STATE_DIR=" + state.env.StateDir,
	}
}

func panelCommandArgs(state *app, self string) []string {
	args := append(sessionEnvArgs(state), "--", self, "panel")

	return append(args, configArgs(state.configStack)...)
}

// hostPath asks the user's own server where its client is sitting, which only a command running
// there can do.
func hostPath(socket string) string {
	if socket == "" {
		dir, err := os.Getwd()
		if err != nil {
			return ""
		}

		return dir
	}

	return tmux.NewClientAtPath(socket).GetClientPath()
}

// refuseNesting stops a panel opening inside an agent's own pane.
func refuseNesting(state *app) error {
	if state.env.AgentID == "" {
		return nil
	}

	return fmt.Errorf(
		"this is agent %s's own pane, so opening a panel here would nest one komrad inside "+
			"another: open it from your own terminal instead", state.env.AgentID)
}

// prepareAutoSetup reports whether auto_setup runs for this open, starting the server it needs.
func prepareAutoSetup(ctx context.Context, state *app) bool {
	if !state.autoSetup || state.configErr != nil || state.engine().IsRemote() {
		return false
	}

	if err := ensureServer(ctx, ensureServerParams{
		Addr:       state.env.Addr,
		Self:       getSelfPath(),
		LogPath:    state.env.LogPath(),
		ConfigArgs: configArgs(state.configStack),
	}); err != nil {
		warnAutoSetup(ctx, state, err)

		return false
	}

	return true
}

// setUpWorkflowHere runs auto_setup for the directory komrad was opened in.
func setUpWorkflowHere(ctx context.Context, state *app) {
	dir, err := os.Getwd()
	if err != nil {
		warnAutoSetup(ctx, state, err)

		return
	}

	base := "http://" + state.env.Addr
	httpClient := xhttp.NewClient(0)
	if err := autoSetup(ctx, autoSetupParams{
		Dir:       dir,
		Git:       git.NewClient(git.NewClientParams{}),
		Workflows: workflowv1connect.NewWorkflowServiceClient(httpClient, base),
		Agents:    agentv1connect.NewAgentServiceClient(httpClient, base),
	}); err != nil {
		warnAutoSetup(ctx, state, err)
	}
}

func warnAutoSetup(ctx context.Context, state *app, err error) {
	fmt.Fprintf(os.Stderr, "komrad: auto_setup skipped: %v\n", err)

	file, openErr := openLogFile(state.env.LogPath())
	if openErr != nil {
		return
	}
	defer func() { _ = file.Close() }()

	logging.New(file, slog.LevelWarn).With(slog.String(logging.ProcessName, logging.ProcessOpen)).
		LogAttrs(ctx, slog.LevelWarn, "auto_setup skipped", logging.Err(err)...)
}
