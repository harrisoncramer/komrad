package cli

import (
	"net/http"

	"weave/internal/agents"
	"weave/internal/errui"
	"weave/internal/ui"
	"weave/proto/gen/action/v1/actionv1connect"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"github.com/spf13/cobra"
)

// panelCmd runs the panel. It is hidden because it is the manager session's own command,
// and by hand it would put a second panel outside the session and popup it expects.
func panelCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "panel",
		Short: "Run the panel itself, as the manager session's own command",
		Long: "Run the panel itself.\n\n" +
			"This is the command the manager tmux session runs, not the one to type. It starts\n" +
			"the service if nothing is answering, then draws the panel and talks to that service\n" +
			"over the API like any other client. Run by hand outside the manager session it will\n" +
			"draw, but the popup and the key bindings around it will not be there.\n\n" +
			"Use `weave open` to bring the panel up.",
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Carried into the panel rather than returned, since returning ends the
			// session and tmux tears the pane down with the message still on it.
			logPath := state.env.LogPath()
			startupErr := ensureServer(ensureServerParams{
				Addr:       state.env.Addr,
				Self:       getSelfPath(),
				LogPath:    logPath,
				ConfigPath: state.configPath,
			})

			// Only on failure. The checks reach a database and a tmux server, which
			// is not work to do on every open, and there is nothing to explain when
			// the server came up.
			var startupFindings []errui.Finding
			if startupErr != nil {
				startupFindings = state.diagnose(cmd.Context())
			}

			base := "http://" + state.env.Addr
			agentClient := agentv1connect.NewAgentServiceClient(http.DefaultClient, base)
			workflowClient := workflowv1connect.NewWorkflowServiceClient(http.DefaultClient, base)
			scheduleClient := schedulev1connect.NewScheduleServiceClient(http.DefaultClient, base)
			actionClient := actionv1connect.NewActionServiceClient(http.DefaultClient, base)

			// Drop the status bar so the panel and its stage fill the popup.
			state.popup.SilenceStatus(agents.ManagerSession)

			// Close and quit detach the popup rather than exit, so Run returns only on a
			// real error.
			return ui.Run(ui.Deps{
				PopupWindow:     state.popup,
				Tmux:            state.tmux,
				Agents:          agentClient,
				Workflows:       workflowClient,
				Schedules:       scheduleClient,
				Keys:            state.keys,
				Actions:         actionClient,
				Marks:           state.marks,
				Layout:          state.layout,
				Addr:            state.env.Addr,
				LogPath:         logPath,
				LogViewer:       state.logViewer,
				ConfigErr:       state.configErr,
				StartupErr:      startupErr,
				StartupFindings: startupFindings,
				Pane:            state.env.TmuxPane,
			})
		},
	}
}
