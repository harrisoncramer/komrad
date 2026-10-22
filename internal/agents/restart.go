package agents

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"komrad/internal/agenterr"
	"komrad/internal/fail"
	"komrad/internal/harness"
	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"

	"github.com/google/uuid"
)

// restartPrompt is what a restarted writer is asked on the first turn of its new conversation.
const restartPrompt = "You carry on with this task on a new conversation, and your earlier one is in your " +
	"transcript. Read the task's notes and run git status before you change anything."

// orchestratorRestartPrompt is what a restarted orchestrator is asked on the first turn of its new conversation.
const orchestratorRestartPrompt = "You carry on as this workflow's orchestrator on a new conversation, and your " +
	"earlier one is in your transcript, which get_transcript reads. Look at the workflow and the agents in it, " +
	"then say what you see and wait."

var (
	// errRestartRole is a restart of an agent that is neither a task's writer nor a workflow's orchestrator.
	errRestartRole = fail.Precondition("only a task's writer or a workflow's orchestrator restarts on a new conversation")
	// errRestartOrchestratorByAgent is an agent restarting an orchestrator.
	errRestartOrchestratorByAgent = fail.Precondition("an agent may not restart an orchestrator")
)

// transcriptSyncer copies what an agent has said so far into the store.
type transcriptSyncer interface {
	SyncAgent(ctx context.Context, agent agentstore.Agent) error
}

// RestartAgentParams is the writer or orchestrator to restart, what it is told on top of its
// briefing, and the CLI and flags it moves onto, blank for the ones it runs.
type RestartAgentParams struct {
	ID      uuid.UUID
	Note    string
	Harness string
	Flags   string
	// ByAgentID is the agent asking, empty for the panel.
	ByAgentID uuid.UUID
}

// RestartAgent relaunches a task's writer or a workflow's orchestrator on a new conversation, its
// earlier conversation kept in its transcript.
func (s *ResumeService) RestartAgent(ctx context.Context, params RestartAgentParams) (*agentstore.Agent, error) {
	agent, err := s.getByID(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	if err := refuseRestart(*agent, params.ByAgentID); err != nil {
		return nil, err
	}
	if err := s.refuseRemovedWorktree(ctx, *agent); err != nil {
		return nil, err
	}

	done, taken := s.takeInFlight(agent.ID)
	if !taken {
		return nil, fmt.Errorf("%w: %q is already being resumed", agenterr.ErrResumeInFlight, agent.Name)
	}
	defer done()

	release, err := s.lockTree(*agent)
	if err != nil {
		return nil, err
	}
	defer release()

	wanted := wantedHarness(*agent, params.Harness)
	hn, err := s.harnesses.GetByID(wanted)
	if err != nil {
		return nil, err
	}
	var task *taskstore.Task
	if isTaskWriter(*agent) {
		if task, err = s.tasks.GetAgentTask(ctx, *agent); err != nil {
			return nil, fmt.Errorf("failed to read the task of %q: %w", agent.Name, err)
		}
	}

	killOwnedPanes(ctx, s.tmux, []agentstore.Agent{*agent})
	s.syncTranscript(ctx, *agent)

	earlier := *agent
	wf := workflowFor(ctx, s.workflows, *agent)
	s.launchService.moveOnto(moveOntoParams{
		agent:    agent,
		workflow: wf,
		harness:  wanted,
		flags:    params.Flags,
	})
	agent.SessionID = harness.MintSessionID(hn)

	if err := s.relaunch(ctx, relaunchParams{
		agent:   *agent,
		harness: hn,
		restart: &restartLaunch{
			prompt:   buildRestartPrompt(*agent, params.Note),
			task:     task,
			workflow: wf,
			spawner:  s.findSpawner(ctx, *agent),
			earlier:  earlier,
		},
	}); err != nil {
		return nil, err
	}

	return s.getByID(ctx, params.ID)
}

// refuseRestart turns away an agent that has no restart, or a restart the caller may not make.
func refuseRestart(agent agentstore.Agent, byAgentID uuid.UUID) error {
	switch {
	case agent.Role == agentstore.RoleOrchestrator:
		if byAgentID != uuid.Nil {
			return errRestartOrchestratorByAgent.Saying("%q is a workflow's orchestrator", agent.Name)
		}
		if agent.ArchivedAt != nil {
			return fmt.Errorf("%w: %q is archived, restore its workflow first", agenterr.ErrAgentArchived, agent.Name)
		}
	case isTaskWriter(agent):
		if agent.ArchivedAt != nil {
			return fmt.Errorf("%w: %q is archived, restore its task first", agenterr.ErrAgentArchived, agent.Name)
		}
	default:
		return errRestartRole.Saying("%q is neither a task's writer nor a workflow's orchestrator", agent.Name)
	}

	return nil
}

// syncTranscript copies the last of the conversation the agent is leaving into the store, which
// stops reading it once the agent moves on.
func (s *ResumeService) syncTranscript(ctx context.Context, agent agentstore.Agent) {
	if err := s.transcripts.SyncAgent(ctx, agent); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to copy the last of a restarted agent's conversation",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID.String())}, logging.Err(err)...)...)
	}
}

// findSpawner returns the agent that spawned this one, or nil when none did or it is gone.
func (s *ResumeService) findSpawner(ctx context.Context, agent agentstore.Agent) *agentstore.Agent {
	if agent.SpawnedByAgentID == nil {
		return nil
	}

	spawner, err := s.agents.GetByID(ctx, *agent.SpawnedByAgentID)
	if err != nil {
		return nil
	}

	return spawner
}

// buildRestartPrompt is the restarted agent's first prompt, with the note it was restarted with.
func buildRestartPrompt(agent agentstore.Agent, note string) string {
	prompt := restartPrompt
	if agent.Role == agentstore.RoleOrchestrator {
		prompt = orchestratorRestartPrompt
	}
	if note = strings.TrimSpace(note); note != "" {
		return prompt + "\n\n" + note
	}

	return prompt
}

// restartLaunch is what an agent restarting on a new conversation is briefed with.
type restartLaunch struct {
	prompt   string
	task     *taskstore.Task
	workflow *workflowstore.Workflow
	spawner  *agentstore.Agent
	earlier  agentstore.Agent
}

// plan is the agent's launch on its new conversation, briefed the way it was when it was made.
func (f *restartLaunch) plan(hn harness.Harness, agent agentstore.Agent) paneLaunch {
	launch := launchParams{
		Pane:         agent.Pane,
		Dir:          agent.Cwd,
		Flags:        agent.Flags,
		Prompt:       f.prompt,
		SessionID:    agent.SessionID,
		AgentID:      agent.ID,
		Role:         agent.Role,
		OwnsCheckout: isTaskWriter(agent),
		Task:         f.task,
	}
	if f.workflow != nil {
		launch.WorkflowContext = f.workflow.Context
	}
	if f.spawner != nil {
		launch.SpawnedByAgentID = f.spawner.ID
		launch.SpawnedByName = f.spawner.Name
	}

	return paneLaunch{
		harness: hn,
		params:  launch,
		verb:    "restart",
	}
}
