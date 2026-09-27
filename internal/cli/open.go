package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"komrad/config"
	"komrad/internal/agents"
	"komrad/internal/tmux"
	"komrad/internal/version"
	"komrad/lib/xslice"

	"github.com/spf13/cobra"
)

// openFlags is what an open is given beyond the engine, the last three only by a panel handing
// its terminal to another engine.
type openFlags struct {
	detach   bool
	headless bool
	host     hostPlace
	fallback string
}

// hostPlace is where the user's own tmux client sits, empty when it is to be read from the
// environment.
type hostPlace struct {
	socket string
	path   string
}

func openCmd(state *app) *cobra.Command {
	var flags openFlags

	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the panel in this terminal, creating or attaching its persistent session",
		Long: `Open the panel in this terminal.

It creates the manager session if it is not there and attaches either way, so the
panel keeps its place between opens rather than starting over. The manager session
lives on komrad's own tmux server, so it does not show up in ` + "`tmux ls`" + `.

It opens this machine's panel, or the engine --engine names. From inside a panel,
o then e moves the terminal to another engine's.

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
			err := runOpen(cmd.Context(), state, flags)
			if err == nil || flags.fallback == "" {
				return err
			}

			return fallBack(fallBackParams{
				self:  state.selfPath(),
				paths: state.configStack.Paths,
				from:  state.configStack.Engine,
				flags: flags,
				err:   err,
			})
		},
	}

	cmd.Flags().BoolVar(&flags.detach, "detach", false, "create the manager session if it is not there, then return without attaching")
	cmd.Flags().BoolVar(&flags.headless, "headless", false, "create the manager session with no panel in it, for an engine driven from another machine")
	cmd.Flags().StringVar(&flags.host.socket, hostSocketFlag, "", "the tmux socket the user's own client is on")
	cmd.Flags().StringVar(&flags.host.path, hostPathFlag, "", "the directory the user's own client is in")
	cmd.Flags().StringVar(&flags.fallback, fallbackEngineFlag, "", "the engine to open again when this open fails")

	for _, name := range []string{hostSocketFlag, hostPathFlag, fallbackEngineFlag} {
		if err := cmd.Flags().MarkHidden(name); err != nil {
			panic(err)
		}
	}

	return cmd
}

const (
	hostSocketFlag     = "host-socket"
	hostPathFlag       = "host-path"
	fallbackEngineFlag = "fallback-engine"
)

func runOpen(ctx context.Context, state *app, flags openFlags) error {
	if !flags.detach && !flags.headless {
		if err := refuseNesting(state); err != nil {
			return err
		}
	}

	return openHere(ctx, state, flags)
}

// fallBackParams is a failed open and the engine to go back to.
type fallBackParams struct {
	self  string
	paths []string
	from  string
	flags openFlags
	err   error
}

// fallBack says why the open failed and, once the user has read it, opens the engine the
// terminal came from.
func fallBack(params fallBackParams) error {
	fmt.Fprintf(os.Stderr, "could not open %s: %v\npress enter to go back to %s\n", engineLabel(params.from), params.err, params.flags.fallback)

	if _, readErr := bufio.NewReader(os.Stdin).ReadString('\n'); readErr != nil {
		return params.err
	}

	args := switchEngineArgs(switchEngineArgsParams{
		self:  params.self,
		paths: params.paths,
		to:    params.flags.fallback,
		host:  params.flags.host,
	})

	if execErr := syscall.Exec(args[0], args, os.Environ()); execErr != nil {
		return fmt.Errorf("failed to go back to %s: %w", params.flags.fallback, execErr)
	}

	return nil
}

// fallBackFromLoad is fallBack for an open that failed while its config was still being read.
func fallBackFromLoad(cmd *cobra.Command, paths []string, engine string, err error) error {
	if cmd.Name() != "open" {
		return err
	}

	var flags openFlags
	for name, value := range map[string]*string{
		fallbackEngineFlag: &flags.fallback,
		hostSocketFlag:     &flags.host.socket,
		hostPathFlag:       &flags.host.path,
	} {
		read, flagErr := cmd.Flags().GetString(name)
		if flagErr != nil {
			return err
		}

		*value = read
	}

	if flags.fallback == "" {
		return err
	}

	return fallBack(fallBackParams{self: getSelfPath(), paths: paths, from: engine, flags: flags, err: err})
}

// engineLabel names an engine as the user types it, local for this machine.
func engineLabel(name string) string {
	if name == "" {
		return config.LocalEngine
	}

	return name
}

// switchEngineArgsParams is what an open on another engine is run with.
type switchEngineArgsParams struct {
	self     string
	paths    []string
	to       string
	host     hostPlace
	fallback string
}

// switchEngineArgs is the argv that opens another engine in the terminal a panel hands over.
func switchEngineArgs(params switchEngineArgsParams) []string {
	args := []string{params.self, "open", "--engine", params.to}

	for _, path := range params.paths {
		args = append(args, "--config", path)
	}

	if params.host.socket != "" {
		args = append(args, "--"+hostSocketFlag, params.host.socket)
	}

	if params.host.path != "" {
		args = append(args, "--"+hostPathFlag, params.host.path)
	}

	if params.fallback != "" {
		args = append(args, "--"+fallbackEngineFlag, params.fallback)
	}

	return args
}

// openHere puts this engine's panel up and turns this process into the client attached to it.
func openHere(ctx context.Context, state *app, flags openFlags) error {
	if err := ensureManagerSession(ctx, state, flags.detach, flags.headless); err != nil {
		return err
	}

	state.tmux.BlankBorders()
	state.tmux.InsetPanelWindow(agents.ManagerSession)

	if flags.detach || flags.headless {
		return nil
	}

	// Only the attaching path says which window is current.
	state.tmux.SelectWindow(state.tmux.GetPanelPane())

	host := flags.host
	if host.socket == "" {
		host.socket = tmux.SocketFromEnv(state.env.Tmux)
	}

	if host.path == "" {
		host.path = hostPath(host.socket)
	}

	state.tmux.RecordHostSocket(host.socket)
	state.tmux.RecordHostPath(host.path)

	return attachManagerSession(state)
}

// attachManagerSession replaces this process with the tmux client.
func attachManagerSession(state *app) error {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("failed to find tmux: %w", err)
	}

	args := []string{"tmux", "-L", state.env.TmuxSocket, "attach-session", "-t", agents.ManagerSession}
	if err := syscall.Exec(path, args, envWithoutTmux()); err != nil {
		return fmt.Errorf("failed to attach the panel: %w", err)
	}

	return nil
}

func isManagerSessionRunning(ctx context.Context, state *app) bool {
	return state.tmuxCommand(ctx, "has-session", "-t", agents.ManagerSession).Run() == nil
}

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
		evictPanel(state)
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

// evictPanel takes down the window the panel is drawing in, first putting any agent's pane it
// had swapped in back into a window of its own.
func evictPanel(state *app) {
	panel := state.tmux.GetPanelPane()

	for _, pane := range state.tmux.GetSiblingPanes(panel) {
		if state.tmux.GetPaneOwner(pane) != "" {
			state.tmux.BreakPane(pane)
		}
	}

	state.tmux.KillWindow(panel)
}

// silenceManagerPrefix leaves the engine's own server answering to no prefix.
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
