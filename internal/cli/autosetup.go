package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	agentv1 "komrad/proto/gen/agent/v1"
	paginationv1 "komrad/proto/gen/pagination/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
)

const autoSetupPrompt = "You are the default orchestrator for this directory. Please stand by for instructions."

type autoSetupWorkflows interface {
	ListWorkflows(ctx context.Context, req *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error)
	CreateWorkflow(ctx context.Context, req *connect.Request[workflowv1.CreateWorkflowRequest]) (*connect.Response[workflowv1.CreateWorkflowResponse], error)
}

type autoSetupAgents interface {
	StartOrchestrator(ctx context.Context, req *connect.Request[agentv1.StartOrchestratorRequest]) (*connect.Response[agentv1.StartOrchestratorResponse], error)
}

type autoSetupGit interface {
	GetRoot(ctx context.Context, dir string) (string, error)
}

type autoSetupParams struct {
	Dir       string
	Git       autoSetupGit
	Workflows autoSetupWorkflows
	Agents    autoSetupAgents
}

// autoSetup gives the checkout holding dir a workflow cutting worktrees and an orchestrator, unless
// a workflow already starts there.
func autoSetup(ctx context.Context, params autoSetupParams) error {
	root, found := checkoutRoot(ctx, params.Git, params.Dir)
	if !found {
		return nil
	}

	covered, err := isWorkflowAt(ctx, params.Workflows, root, filepath.Clean(params.Dir))
	if err != nil || covered {
		return err
	}

	created, err := params.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     filepath.Base(root),
		Cwd:      root,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true},
	}))
	if err != nil {
		return fmt.Errorf("failed to create a workflow for %s: %w", root, err)
	}

	if _, err := params.Agents.StartOrchestrator(ctx, connect.NewRequest(&agentv1.StartOrchestratorRequest{
		WorkflowId: created.Msg.GetWorkflow().GetId(),
		Prompt:     autoSetupPrompt,
	})); err != nil {
		return fmt.Errorf("failed to start the orchestrator for %s: %w", root, err)
	}

	return nil
}

func checkoutRoot(ctx context.Context, repo autoSetupGit, dir string) (string, bool) {
	root, err := repo.GetRoot(ctx, dir)

	return root, err == nil
}

func isWorkflowAt(ctx context.Context, workflows autoSetupWorkflows, dirs ...string) (bool, error) {
	var cursor *string
	for {
		resp, err := workflows.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{
			Pagination: &paginationv1.Pagination{Cursor: cursor},
		}))
		if err != nil {
			return false, fmt.Errorf("failed to list workflows: %w", err)
		}

		for _, workflow := range resp.Msg.GetWorkflows() {
			if slices.Contains(dirs, filepath.Clean(workflow.GetCwd())) {
				return true, nil
			}
		}

		if resp.Msg.GetNextPageToken() == "" {
			return false, nil
		}
		cursor = resp.Msg.NextPageToken
	}
}
