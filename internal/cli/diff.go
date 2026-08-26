package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"weave/internal/diff"
	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

func diffCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <agent>",
		Short: "Review what an agent has changed in its worktree",
		Long: "Review what an agent has changed in its worktree.\n\n" +
			"The diff is measured against the point the agent's branch left its workflow's base\n" +
			"branch, so committed and uncommitted work show together, along with any file the\n" +
			"agent has written and not yet added.\n\n" +
			"The panel binds this to a key on the agents list, which opens it as a popup over\n" +
			"whatever you are looking at. Move with j and k, jump between files with ] and [,\n" +
			"pan sideways with h and l, and press q to close.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := loadAgentDiff(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}

			return review.Run(review.RunParams{
				Patch:     loaded.patch,
				AgentName: loaded.agentName,
				Threads:   loaded.threads,
			})
		},
	}
}

type loadedDiff struct {
	patch *diff.Patch

	agentName string

	threads review.ThreadClient
}

func loadAgentDiff(ctx context.Context, state *app, agentID string) (*loadedDiff, error) {
	base := "http://" + state.env.Addr

	agents := agentv1connect.NewAgentServiceClient(http.DefaultClient, base)
	workflows := workflowv1connect.NewWorkflowServiceClient(http.DefaultClient, base)

	found, err := agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agentID}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the agent %q: %w", agentID, err)
	}

	agent := found.Msg.GetAgent()
	if agent.GetCwd() == "" {
		return nil, errors.New("this agent has no directory of its own, so there is nothing to diff")
	}
	if agent.GetWorktreeRemoved() {
		return nil, errors.New("this agent's worktree has been removed, so there is nothing to diff")
	}

	workflow, err := workflows.GetWorkflow(ctx,
		connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: agent.GetWorkflowId()}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the workflow of %q: %w", agentID, err)
	}

	patch, err := diff.Load(ctx, diff.LoadParams{
		Dir:  agent.GetCwd(),
		Base: workflow.Msg.GetWorkflow().GetWorktree().GetBaseBranch(),
	})
	if err != nil {
		return nil, err
	}

	return &loadedDiff{
		patch:     patch,
		agentName: agent.GetName(),
		threads:   newReviewThreadClient(agents, agentID),
	}, nil
}
