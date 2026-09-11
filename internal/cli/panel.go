package cli

import (
	"log/slog"
	"net/http"

	"weave/internal/agents"
	"weave/internal/errui"
	"weave/internal/logging"
	"weave/internal/ui"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/hook/v1/hookv1connect"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"github.com/spf13/cobra"
)

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

			var startupErr error
			if state.configErr == nil {
				startupErr = ensureServer(cmd.Context(), ensureServerParams{
					Addr:        state.env.Addr,
					Self:        getSelfPath(),
					LogPath:     logPath,
					ConfigPaths: state.configPaths,
				})
			}

			var startupFindings []errui.Finding
			if startupErr != nil {
				startupFindings = state.diagnose(cmd.Context())
			}

			base := "http://" + state.env.Addr
			agentClient := agentv1connect.NewAgentServiceClient(http.DefaultClient, base)
			workflowClient := workflowv1connect.NewWorkflowServiceClient(http.DefaultClient, base)
			scheduleClient := schedulev1connect.NewScheduleServiceClient(http.DefaultClient, base)
			hookClient := hookv1connect.NewHookServiceClient(http.DefaultClient, base)

			state.popup.SilenceStatus(agents.ManagerSession)
			state.tmux.EnableFocusEvents()

			return ui.Run(ui.Deps{
				PopupWindow:     state.popup,
				Tmux:            state.tmux,
				Git:             ui.GitClient{},
				HarnessFrames:   paneFrameReader{panes: state.tmux, harnesses: state.harnesses},
				Agents:          agentClient,
				Workflows:       workflowClient,
				Schedules:       scheduleClient,
				Keys:            state.keys,
				Hooks:           hookClient,
				Layout:          state.layout,
				Addr:            state.env.Addr,
				LogPath:         logPath,
				LogViewer:       state.logViewer,
				ProcessViewer:   state.processViewer,
				Editor:          state.env.Editor,
				RunLogsDir:      state.env.RunLogsDir(),
				RoundsDir:       state.env.RoundsDir(),
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
