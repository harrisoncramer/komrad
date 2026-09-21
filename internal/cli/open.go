package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"weave/internal/agents"
	"weave/internal/tmux"
	"weave/internal/ui"
	"weave/internal/version"
	"weave/lib/xslice"

	"github.com/spf13/cobra"
)

func openCmd(state *app) *cobra.Command {
	var (
		detach   bool
		headless bool
	)

	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the panel popup, creating or attaching its persistent session",
		Long: `Open the panel popup over the current tmux session.

It creates the manager session if it is not there and attaches either way, so the
panel keeps its place between opens rather than starting over. The manager session
lives on weave's own tmux server, so it does not show up in ` + "`tmux ls`" + `.

The tmux plugin runs this as the popup's command, so it blocks until the client
detaches, which is what closes the popup. Pressing the panel key again runs
` + "`weave hide`" + `. Run by hand outside a popup it attaches the manager session in the
terminal instead, which is the panel without the popup around it.

With --detach it creates the session and returns, which is what a script wants when
it needs somewhere for agents to be put back into.

With --headless it creates that session holding nothing but a placeholder, for an
engine whose panel runs on somebody else's machine and which would otherwise swap
an agent's pane into a panel nobody is looking at.`,
		Args:          cobra.ArbitraryArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := refuseNesting(state); err != nil {
				return err
			}

			if err := ensureManagerSession(cmd.Context(), state, detach, headless); err != nil {
				return err
			}

			if detach || headless {
				return nil
			}

			// Only the attaching path says which window is current, since select-window moves every
			// client attached to the session and nobody asked a detached open to move them.
			state.tmux.SelectWindow(state.tmux.GetPanelPane())

			socket := tmux.SocketFromEnv(state.env.Tmux)
			state.tmux.RecordHostSocket(socket)
			state.tmux.RecordHostPath(hostPath(socket))

			state.popup.BindHide(userPanelKey(state, socket))

			return attachManagerSession(cmd.Context(), state)
		},
	}

	cmd.Flags().BoolVar(&detach, "detach", false, "create the manager session if it is not there, then return without attaching")
	cmd.Flags().BoolVar(&headless, "headless", false, "create the manager session with no panel in it, for an engine driven from another machine")

	return cmd
}

func attachManagerSession(ctx context.Context, state *app) error {
	attach := state.tmuxCommand(ctx, "attach-session", "-t", agents.ManagerSession)
	attach.Stdin = os.Stdin
	attach.Stdout = os.Stdout
	attach.Stderr = os.Stderr

	if err := attach.Run(); err != nil {
		return fmt.Errorf("failed to attach the panel: %w", err)
	}

	return nil
}

func isManagerSessionRunning(ctx context.Context, state *app) bool {
	return state.tmuxCommand(ctx, "has-session", "-t", agents.ManagerSession).Run() == nil
}

// Explainer: the manager session holds a window per agent as well as the panel's own, so it
// answers has-session long after the panel process has gone. Asking about the session alone
// therefore reports a panel that is not there, and an open on that answer attaches a session
// with no panel in it.

// isPanelRunning reports whether a panel is drawing in the manager session.
func isPanelRunning(ctx context.Context, state *app) bool {
	if !isManagerSessionRunning(ctx, state) {
		return false
	}

	return state.tmux.IsPaneAlive(state.tmux.GetPanelPane())
}

// isPanelStale reports whether the panel drawing in the manager session was built from other
// code than this binary, a panel that recorded no build counting as one.
func isPanelStale(ctx context.Context, state *app, build string) bool {
	return isPanelRunning(ctx, state) && state.tmux.GetPanelBuild() != build
}

// ensureManagerSession puts a panel up, making the session when there is none and a window in
// the session it already has when only the panel has gone.
func ensureManagerSession(ctx context.Context, state *app, detached, headless bool) error {
	if !headless && isPanelStale(ctx, state, version.Describe(version.Version(), version.Commit())) {
		state.tmux.KillPane(state.tmux.GetPanelPane())
	}

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

// silenceManagerPrefix leaves the engine's own server answering to no prefix, since a headless
// session is a transport for a panel elsewhere rather than something anybody drives from here.
func silenceManagerPrefix(state *app) {
	state.tmux.SetGlobal("prefix", "None")
	state.tmux.SetGlobal("prefix2", "None")
}

func runTmux(ctx context.Context, state *app, summary string, args []string) error {
	cmd := state.tmuxCommand(ctx, args...)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
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

	return append(append(args, "-t", agents.ManagerSession), panelCommandArgs(state, self)...)
}

func sessionEnvArgs(state *app) []string {
	return []string{
		"-e", "WEAVE_TMUX_SOCKET=" + state.env.TmuxSocket,
		"-e", "WEAVE_DATABASE_URL=" + state.env.DatabaseURL,
		"-e", "WEAVE_READONLY_DATABASE_URL=" + state.env.ReadOnlyDatabaseURL,
		"-e", "WEAVE_ADDR=" + state.env.Addr,
		"-e", "EDITOR=" + state.env.Editor,
		"-e", "WEAVE_CACHE_DIR=" + state.env.CacheDir,
	}
}

func panelCommandArgs(state *app, self string) []string {
	args := append(sessionEnvArgs(state), "--", self, "panel")

	return append(args, configArgs(state.configStack)...)
}

func (a *app) tmuxCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "tmux", append([]string{"-L", a.env.TmuxSocket}, args...)...)
	cmd.Env = envWithoutTmux()

	return cmd
}

func envWithoutTmux() []string {
	return xslice.Filter(os.Environ(), func(entry string) bool { return !strings.HasPrefix(entry, "TMUX=") })
}

// userPanelKey asks whichever server the user is at, this process's own when it is running in
// tmux and otherwise the one the last open recorded, and takes the configured key over it.
func userPanelKey(state *app, socket string) string {
	if socket == "" {
		socket = state.tmux.GetHostSocket()
	}

	if socket == "" {
		return state.env.PanelKey
	}

	return ui.PanelKey(tmux.NewClientAtPath(socket), state.env.PanelKey)
}

// hostPath asks the user's own server where its client is sitting, which only a command running
// there can do.
func hostPath(socket string) string {
	if socket == "" {
		return ""
	}

	return tmux.NewClientAtPath(socket).GetClientPath()
}

// refuseNesting stops a panel opening inside an agent's own pane.
func refuseNesting(state *app) error {
	if state.env.AgentID == "" {
		return nil
	}

	return fmt.Errorf(
		"this is agent %s's own pane, so opening a panel here would nest one weave inside "+
			"another: open it from your own terminal instead", state.env.AgentID)
}
