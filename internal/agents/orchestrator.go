package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrade/db"
	"komrade/internal/store"
	agentstore "komrade/internal/store/agent"
)

const orchestratorDirName = "orchestrators"

// orchestratorPrompt is the opening prompt every orchestrator is launched with.
const orchestratorPrompt = `You are this workflow's orchestrator, and nobody has asked you for anything yet.

Have a look at the workflow you stand behind and at the agents already in it, then say what you see and wait. Do not spawn anything until somebody asks you to.`

type orchestratorAgentReader interface {
	GetWorkflowOrchestrator(ctx context.Context, workflowID string) (*agentstore.Agent, error)
}

type orchestratorTranscriptReader interface {
	Get(ctx context.Context, agentID string) (string, error)
}

type NewOrchestratorServiceParams struct {
	Agents      orchestratorAgentReader
	Workflows   workflowReader
	Transcripts orchestratorTranscriptReader
	Harnesses   harnessGetter
	Launch      *LaunchService
	Lifecycle   *LifecycleService
	Resume      *ResumeService
	StateDir    string
}

// OrchestratorService brings a workflow's orchestrator back, or starts the one it has never
// had.
type OrchestratorService struct {
	agents      orchestratorAgentReader
	workflows   workflowReader
	transcripts orchestratorTranscriptReader
	harnesses   harnessGetter
	launch      *LaunchService
	lifecycle   *LifecycleService
	resume      *ResumeService
	stateDir    string
}

// NewOrchestratorService builds the service over the stores and the services it drives.
func NewOrchestratorService(params NewOrchestratorServiceParams) *OrchestratorService {
	return &OrchestratorService{
		agents:      params.Agents,
		workflows:   params.Workflows,
		transcripts: params.Transcripts,
		harnesses:   params.Harnesses,
		launch:      params.Launch,
		lifecycle:   params.Lifecycle,
		resume:      params.Resume,
		stateDir:    params.StateDir,
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

	// A row whose conversation never started cannot be resumed into one.
	spoken, err := s.hasSpoken(ctx, *found)
	if err != nil {
		return nil, err
	}
	// A row still holding a pane is running, whether or not it has said anything yet.
	if !spoken && found.Pane == "" {
		if err := s.lifecycle.DeleteAgents(ctx, []string{found.ID}); err != nil {
			return nil, fmt.Errorf("failed to clear the orchestrator of workflow %q, which never ran: %w", workflowID, err)
		}

		return s.launchOrchestrator(ctx, workflowID)
	}

	if found.ArchivedAt != nil {
		if _, err := s.lifecycle.RestoreAgents(ctx, []string{found.ID}); err != nil {
			return nil, fmt.Errorf("failed to restore the orchestrator of workflow %q: %w", workflowID, err)
		}
	}

	return s.resume.ResumeAgent(ctx, found.ID)
}

// hasSpoken reports whether an orchestrator ever held a conversation.
func (s *OrchestratorService) hasSpoken(ctx context.Context, agent agentstore.Agent) (bool, error) {
	stored, err := s.transcripts.Get(ctx, agent.ID)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(stored) != "" {
		return true, nil
	}

	if agent.SessionID == "" {
		return false, nil
	}

	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return false, err
	}

	return hn.LogPath(agent.SessionID) != "", nil
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

// orchestratorDir is the scratch directory an orchestrator runs in, deliberately not a checkout.
func (s *OrchestratorService) orchestratorDir(workflowID string) (string, error) {
	dir := filepath.Join(s.stateDir, orchestratorDirName, workflowID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to make a directory for the orchestrator of workflow %q: %w", workflowID, err)
	}

	return dir, nil
}
