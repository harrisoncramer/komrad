package cli

import (
	"log/slog"
	"time"

	"weave/internal/agents"
	"weave/internal/errui"
	"weave/internal/git"
	"weave/internal/logging"
	"weave/internal/ui"
	"weave/lib/xhttp"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/hook/v1/hookv1connect"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

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
draw, but the popup and the key bindings around it will not be there.

Use ` + "`weave open`" + ` to bring the panel up.`,
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

			// Explainer: a remote engine is reached through an ssh forward weave owns, so the panel
			// talks to a port on this machine that carries the engine's api. Nothing local is
			// started: the service, the agents and their tmux server all belong to the other
			// machine.
			remote := state.engine()
			addr := state.env.Addr

			var startupErr error

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

			state.popup.SilenceStatus(agents.ManagerSession)
			state.tmux.EnableFocusEvents()

			return ui.Run(ui.Deps{
				PopupWindow:     state.popup,
				Tmux:            state.tmux,
				Git:             git.NewClient(),
				HarnessFrames:   paneFrameReader{panes: state.tmux, harnesses: state.harnesses},
				Agents:          agentClient,
				Workflows:       workflowClient,
				Schedules:       scheduleClient,
				Keys:            state.keys,
				Hooks:           hookClient,
				Layout:          state.layout,
				Engine:          remote,
				Addr:            addr,
				LogPath:         logPath,
				LogViewer:       state.logViewer,
				ProcessViewer:   state.processViewer,
				Editor:          state.env.Editor,
				RunLogsDir:      state.env.RunLogsDir(),
				ConfigErr:       state.configErr,
				StartupErr:      startupErr,
				StartupFindings: startupFindings,
				Pane:            state.env.TmuxPane,
			})
		},
	}
}

// Explainer: the panel's stderr is the tmux pane it draws in, which the alt screen owns and
// which goes when the window closes, so a record written there is never seen and a panic leaves
// no trace at all. The default logger is pointed at the same file the service appends to, which
// is also what main's panic handler writes through.

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
