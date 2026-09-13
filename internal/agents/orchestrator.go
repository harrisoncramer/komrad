package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"weave/db"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
)

const orchestratorDirName = "orchestrators"

// orchestratorPrompt is the opening prompt every orchestrator is launched with.
const orchestratorPrompt = `You stand behind this workflow rather than in front of a piece of work.

You hold no checkout and write no code. The directory you are in is scratch space for your own notes and is not a git repository, so there is nothing here to commit. When work needs doing, spawn an agent into this workflow with create_agent and leave the writing to it.

Your conversation is where this workflow's decisions live. Say why work was split the way it was, and what was tried and abandoned, so a person coming back in a month can read it here.`

type orchestratorAgentReader interface {
	GetWorkflowOrchestrator(ctx context.Context, workflowID string) (*agentstore.Agent, error)
}

// OrchestratorService brings a workflow's orchestrator back, or starts the one it has never
// had.
type OrchestratorService struct {
	agents    orchestratorAgentReader
	workflows workflowReader
	launch    *LaunchService
	lifecycle *LifecycleService
	resume    *ResumeService
	stateDir  string
}

type NewOrchestratorServiceParams struct {
	Agents    orchestratorAgentReader
	Workflows workflowReader
	Launch    *LaunchService
	Lifecycle *LifecycleService
	Resume    *ResumeService
	StateDir  string
}

// NewOrchestratorService builds the service over the stores and the services it drives.
func NewOrchestratorService(params NewOrchestratorServiceParams) *OrchestratorService {
	return &OrchestratorService{
		agents:    params.Agents,
		workflows: params.Workflows,
		launch:    params.Launch,
		lifecycle: params.Lifecycle,
		resume:    params.Resume,
		stateDir:  params.StateDir,
	}
}

// Start returns the workflow's orchestrator with a pane to talk to, restoring it from the
// archive and resuming it as far as either is needed.
func (s *OrchestratorService) Start(ctx context.Context, workflowID string) (*agentstore.Agent, error) {
	found, err := s.agents.GetWorkflowOrchestrator(ctx, workflowID)
	if errors.Is(err, store.ErrNotFound) {
		return s.launchOrchestrator(ctx, workflowID)
	}
	if err != nil {
		return nil, err
	}

	if found.ArchivedAt != nil {
		if _, err := s.lifecycle.RestoreAgents(ctx, []string{found.ID}); err != nil {
			return nil, fmt.Errorf("failed to restore the orchestrator of workflow %q: %w", workflowID, err)
		}
	}

	return s.resume.ResumeAgent(ctx, found.ID)
}

func (s *OrchestratorService) launchOrchestrator(ctx context.Context, workflowID string) (*agentstore.Agent, error) {
	// Ahead of the directory, so nothing is written for a workflow that could never hold an
	// orchestrator.
	if _, err := s.workflows.Get(ctx, workflowID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, workflowID)
		}

		return nil, err
	}

	dir, err := s.orchestratorDir(workflowID)
	if err != nil {
		return nil, err
	}

	provision := false

	started, err := s.launch.NewAgent(ctx, NewAgentParams{
		Title:            "orchestrator",
		Dir:              dir,
		Prompt:           orchestratorPrompt,
		WorkflowID:       workflowID,
		Role:             agentstore.RoleOrchestrator,
		ProvisionEnabled: &provision,
	})
	if db.IsUniqueViolation(err) {
		// Another caller got there first, and what it started is the orchestrator this one asked
		// for.
		return s.agents.GetWorkflowOrchestrator(ctx, workflowID)
	}

	return started, err
}

// orchestratorDir is the scratch directory an orchestrator runs in, which is deliberately not
// a checkout.
func (s *OrchestratorService) orchestratorDir(workflowID string) (string, error) {
	dir := filepath.Join(s.stateDir, orchestratorDirName, workflowID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to make a directory for the orchestrator of workflow %q: %w", workflowID, err)
	}

	return dir, nil
}
