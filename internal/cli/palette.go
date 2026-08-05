package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"weave/internal/actions"
	"weave/internal/palette"
	"weave/internal/tmux"
	"weave/lib/xslice"
	actionv1 "weave/proto/gen/action/v1"
	"weave/proto/gen/action/v1/actionv1connect"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/emptypb"
)

// paletteTimeout bounds each call the palette makes. It is short because a person is waiting
// on the popup with the list still empty.
const paletteTimeout = 5 * time.Second

// errNoAgentInPane is what a palette opened over a pane no agent runs in is refused with. The
// panel's own pane is one of those, since an action takes an agent and the panel is not one.
var errNoAgentInPane = errors.New("no agent is running in this pane, so there is nothing to run an action on")

// errNoInvokingPane is what a palette that cannot tell which pane it was opened over is
// refused with, which is what running it outside tmux looks like.
var errNoInvokingPane = errors.New("the palette is opened by its tmux key binding, which is how it is told which pane to act on")

// paletteCmd is the command palette: one key over an agent's pane, an action out of the
// list, and the popup is gone. It is hidden because the key binding is how it is reached, and
// by hand outside a pane there is no agent for it to act on.
func paletteCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "palette",
		Short: "Fire one of your actions on the agent whose pane this was opened over",
		Long: "Open the command palette over an agent's pane.\n\n" +
			"The panel binds this to a key on its own tmux server, so pressing it inside an\n" +
			"agent's pane offers your actions for that agent without leaving the pane. Type to\n" +
			"narrow the list, enter runs what the cursor is on, and the popup closes.\n\n" +
			"Which agent it acts on comes from the pane the key was pressed in rather than from\n" +
			"anything passed here. Set `@weave_palette_key` in your tmux config to change the key.",
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			agentID, err := findPaneAgent(state)
			if err != nil {
				return err
			}

			base := "http://" + state.env.Addr
			agentClient := agentv1connect.NewAgentServiceClient(http.DefaultClient, base)
			actionClient := actionv1connect.NewActionServiceClient(http.DefaultClient, base)

			agent, err := readPaletteAgent(cmd.Context(), agentClient, agentID)
			if err != nil {
				return err
			}

			listed, err := readPaletteActions(cmd.Context(), actionClient)
			if err != nil {
				return err
			}

			return palette.Run(palette.RunParams{
				Agent: agent.GetName(),
				Items: listed,
				Fire: func(name string) error {
					return firePaletteAction(actionClient, agentID, name)
				},
			})
		},
	}
}

// findPaneAgent is the agent whose pane the palette was opened over. The pane cannot be
// handed in, since tmux expands no format in a popup's command, so it is read back off the
// server the popup came from: TMUX names that server and the session the key was pressed in,
// and the stamp on that session's active pane says which agent is running there.
func findPaneAgent(state *app) (string, error) {
	socket, session := tmux.SocketFromEnv(state.env.Tmux), tmux.SessionFromEnv(state.env.Tmux)
	if socket == "" || session == "" {
		return "", errNoInvokingPane
	}

	server := tmux.NewClientAtPath(socket)

	agentID := server.GetPaneOwner(server.GetActivePane(session))
	if agentID == "" {
		return "", errNoAgentInPane
	}

	return agentID, nil
}

func readPaletteAgent(ctx context.Context, agents agentv1connect.AgentServiceClient, id string) (*agentv1.Agent, error) {
	ctx, cancel := context.WithTimeout(ctx, paletteTimeout)
	defer cancel()

	resp, err := agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the agent in this pane: %w", err)
	}

	return resp.Msg.GetAgent(), nil
}

// readPaletteActions is the user's actions as the service finds them, less the shell they keep
// for other actions to source, which is not a thing to fire on its own.
func readPaletteActions(ctx context.Context, client actionv1connect.ActionServiceClient) ([]palette.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, paletteTimeout)
	defer cancel()

	resp, err := client.ListActions(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		return nil, fmt.Errorf("failed to read your actions: %w", err)
	}

	offered := xslice.Filter(resp.Msg.GetActions(), func(action *actionv1.Action) bool {
		return !actions.IsInternal(action.GetName())
	})

	return xslice.Map(offered, func(action *actionv1.Action) palette.Item {
		return palette.Item{
			Name:        action.GetName(),
			Description: action.GetDescription(),
			Executable:  action.GetExecutable(),
		}
	}), nil
}

// firePaletteAction starts the chosen action. Its context is its own rather than the
// command's, since the popup is what the command is waiting on and firing is the last thing
// it does.
func firePaletteAction(client actionv1connect.ActionServiceClient, agentID, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), paletteTimeout)
	defer cancel()

	_, err := client.RunAction(ctx, connect.NewRequest(&actionv1.RunActionRequest{
		AgentId: agentID,
		Action:  name,
	}))
	if err != nil {
		return fmt.Errorf("failed to run action %s: %w", name, err)
	}

	return nil
}
