package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"komrad/config"
	"komrad/internal/browser"
	"komrad/internal/doctor"
	"komrad/internal/engine"
	"komrad/internal/errui"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/ui"
	"komrad/internal/version"
	"komrad/lib/xhttp"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/forge/v1/forgev1connect"
	"komrad/proto/gen/git/v1/gitv1connect"
	"komrad/proto/gen/hook/v1/hookv1connect"
	"komrad/proto/gen/log/v1/logv1connect"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"
	"komrad/proto/gen/schedule/v1/schedulev1connect"
	"komrad/proto/gen/watch/v1/watchv1connect"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"github.com/spf13/cobra"
)

// tunnelWait is how long the panel waits for an ssh forward to start carrying the engine.
const tunnelWait = 15 * time.Second

func panelCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "panel",
		Short: "Run the panel itself, as the manager session's own command",
		Long: `Run the panel itself.

This is the command the manager tmux session runs, not the one to type. It starts
the service if nothing is answering, then draws the panel and talks to that service
over the API like any other client. Run by hand outside the manager session it will
draw, but the key bindings around it will not be there.

Use ` + "`komrad open`" + ` to bring the panel up.`,
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logPath := state.env.LogPath()

			if err := installPanelLogger(logPath, state.logLevel); err != nil {
				return err
			}

			// Recorded before anything slow: while no pane is on record the session looks like one
			// whose panel has gone, and a second open would start a second panel in it.
			state.tmux.RecordPanelPane(state.env.TmuxPane)
			state.tmux.RecordPanelBuild(version.Describe(version.Version(), version.Commit()))

			if err := state.tmux.TakePanelKeys(state.keys.GetBoundKeys()); err != nil {
				logging.From(cmd.Context()).LogAttrs(cmd.Context(), slog.LevelWarn,
					"a key the panel is bound to stays with tmux", logging.Err(err)...)
			}

			if err := state.tmux.TakePanelWheel(); err != nil {
				logging.From(cmd.Context()).LogAttrs(cmd.Context(), slog.LevelWarn,
					"the mouse wheel stays with tmux over the panel", logging.Err(err)...)
			}

			remote := state.engine()
			addr := state.env.Addr

			var (
				startupErr      error
				shellCommand    []string
				terminalCommand []string
			)

			switch {
			case remote.IsRemote():
				tunnel, err := remote.Dial(cmd.Context(), addr, tunnelWait)
				if err != nil {
					startupErr = err

					break
				}

				defer func() { _ = tunnel.Close() }()

				addr = tunnel.Local
				startupErr = requireEngineBuild(cmd.Context(), addr)
				self := append([]string{state.selfPath()}, configArgs(state.configStack)...)
				shellCommand = append(slices.Clone(self), "shell", "--addr", addr)
				terminalCommand = append(slices.Clone(self), "terminal", "--addr", addr)
			case state.configErr == nil:
				startupErr = ensureServer(cmd.Context(), ensureServerParams{
					Addr:       addr,
					Self:       getSelfPath(),
					LogPath:    logPath,
					ConfigArgs: configArgs(state.configStack),
				})
			}

			var startupFindings []errui.Finding
			if startupErr != nil {
				startupFindings = state.diagnose(cmd.Context())
			}

			base := "http://" + addr
			httpClient := xhttp.NewClient(0)
			agentClient := agentv1connect.NewAgentServiceClient(httpClient, base)
			workflowClient := workflowv1connect.NewWorkflowServiceClient(httpClient, base)
			scheduleClient := schedulev1connect.NewScheduleServiceClient(httpClient, base)
			hookClient := hookv1connect.NewHookServiceClient(httpClient, base)
			worktreeClient := worktreev1connect.NewWorktreeServiceClient(httpClient, base)
			logClient := logv1connect.NewLogServiceClient(httpClient, base)
			forgeClient := forgev1connect.NewForgeServiceClient(httpClient, base)
			pullRequestClient := pullrequestv1connect.NewPullRequestServiceClient(httpClient, base)
			watchClient := watchv1connect.NewWatchServiceClient(httpClient, base)

			state.session.SilenceStatus()
			state.tmux.EnableFocusEvents()
			copier, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to find the komrad binary visual mode copies through: %w", err)
			}
			state.session.TuneCopyMode(copier)

			var binary ui.BinaryWatcher
			if watch := watchExecutable(); watch != nil {
				binary = watch
			}

			return ui.Run(cmd.Context(), ui.Deps{
				Session:         state.session,
				Tmux:            state.tmux,
				Git:             reviewGit(remote, gitv1connect.NewGitServiceClient(httpClient, base)),
				Browser:         browser.NewDesktop(),
				HarnessFrames:   paneFrameReader{panes: state.tmux, harnesses: state.harnesses},
				Agents:          agentClient,
				Workflows:       workflowClient,
				Schedules:       scheduleClient,
				Keys:            state.keys,
				Glyphs:          state.glyphs,
				Loader:          state.loader,
				Hooks:           hookClient,
				Worktrees:       worktreeClient,
				Logs:            logClient,
				Forges:          forgeClient,
				PullRequests:    pullRequestClient,
				Watch:           watchClient,
				Layout:          state.layout,
				Engine:          remote,
				ShellCommand:    shellCommand,
				TerminalCommand: terminalCommand,
				Binary:          binary,
				Settings: ui.PanelSettings{
					Addr:       addr,
					Editor:     state.env.Editor,
					Pane:       state.env.TmuxPane,
					ScratchDir: scratchDir(),
				},
				ConfigErr:       state.configErr,
				StartupErr:      startupErr,
				StartupFindings: startupFindings,
				EngineStatus:    panelEngineStatus(cmd.Context(), state, addr, startupErr),
			})
		},
	}
}

// panelEngineStatus is the engine this panel is on and how to ask what it runs.
func panelEngineStatus(ctx context.Context, state *app, addr string, startupErr error) ui.EngineStatus {
	var build ui.EngineBuild
	if startupErr == nil {
		build, _ = readEngineBuild(ctx, addr)
	}

	return ui.EngineStatus{
		Name:      state.configStack.Engine,
		Build:     build,
		ReadBuild: func(ctx context.Context) (ui.EngineBuild, bool) { return readEngineBuild(ctx, addr) },
	}
}

// readEngineBuild asks the server on addr what it runs, and how that differs from this panel.
func readEngineBuild(ctx context.Context, addr string) (ui.EngineBuild, bool) {
	probe := probeServer(ctx, addr)
	if !probe.answering {
		return ui.EngineBuild{}, false
	}

	build := ui.EngineBuild{Commit: version.Short(probe.revision)}
	if version.IsRelease(probe.version) {
		build.Commit = probe.version
	}
	if probe.version != "" && !probe.current {
		build.Stale = fmt.Sprintf("the engine runs %s and this panel %s: %s",
			version.Describe(probe.version, probe.commit),
			version.Describe(version.Version(), version.Commit()),
			doctor.StaleEngineFix(probe.version, probe.commit))
	}

	return build, true
}

// installPanelLogger points the default logger at the log file for the life of the process.
func installPanelLogger(logPath, level string) error {
	parsed, err := logging.ParseLevel(level)
	if err != nil {
		return err
	}

	// The file is deliberately never closed: main's deferred panic handler logs through it after
	// this command has already returned.
	file, err := openLogFile(logPath)
	if err != nil {
		return err
	}

	slog.SetDefault(logging.New(file, parsed).With(slog.String(logging.ProcessName, logging.ProcessPanel)))

	return nil
}

// reviewGit is the git the review screen reads a round through, on the engine when the agents
// and their worktrees are there.
func reviewGit(remote engine.Remote, engineGit gitv1connect.GitServiceClient) ui.ReviewGit {
	if remote.IsRemote() {
		return git.NewEngineClient(engineGit)
	}

	return git.NewClient()
}

// scratchDir is where the panel keeps each engine's scratch file, empty when there is no home.
func scratchDir() string {
	state, err := config.GetStateDir()
	if err != nil {
		return ""
	}

	return filepath.Join(state, "scratch")
}
