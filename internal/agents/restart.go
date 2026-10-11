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
)

// restartPrompt is what a restarted writer is asked on the first turn of its new conversation.
const restartPrompt = "You carry on with this task on a new conversation, and your earlier one is in your " +
	"transcript. Read the task's notes and run git status before you change anything."

// errRestartNotWriter is a restart of an agent that is not a task's writer.
var errRestartNotWriter = fail.Precondition("only a task's writer restarts on a new conversation")

// transcriptSyncer copies what an agent has said so far into the store.
type transcriptSyncer interface {
	SyncAgent(ctx context.Context, agent agentstore.Agent) error
}

// RestartAgentParams is the writer to restart and what it is told on top of its briefing.
type RestartAgentParams struct {
	ID   string
	Note string
}

// RestartAgent relaunches a task's writer on a new conversation, briefed from its task, its earlier
// conversation kept in its transcript.
func (s *ResumeService) RestartAgent(ctx context.Context, params RestartAgentParams) (*agentstore.Agent, error) {
	agent, err := s.getByID(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	if !isTaskWriter(*agent) {
		return nil, errRestartNotWriter.Saying("%q is not a task's writer", agent.Name)
	}
	if agent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q is archived, restore its task first", agenterr.ErrAgentArchived, agent.Name)
	}
	if err := agent.ValidateActionable(); err != nil {
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

	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return nil, err
	}
	task, err := s.tasks.GetAgentTask(ctx, *agent)
	if err != nil {
		return nil, fmt.Errorf("failed to read the task of %q: %w", agent.Name, err)
	}

	killOwnedPanes(ctx, s.tmux, []agentstore.Agent{*agent})
	s.syncTranscript(ctx, *agent)

	earlier := agent.SessionID
	agent.SessionID = harness.MintSessionID(hn)

	if err := s.relaunch(ctx, relaunchParams{
		agent:   *agent,
		harness: hn,
		fresh: &freshStart{
			prompt:           restartBrief(params.Note),
			task:             task,
			workflow:         workflowFor(ctx, s.workflows, *agent),
			spawner:          s.findSpawner(ctx, *agent),
			earlierSessionID: earlier,
		},
	}); err != nil {
		return nil, err
	}

	return s.getByID(ctx, params.ID)
}

// syncTranscript copies the last of the conversation the writer is leaving into the store, which
// stops reading it once the writer moves on.
func (s *ResumeService) syncTranscript(ctx context.Context, agent agentstore.Agent) {
	if err := s.transcripts.SyncAgent(ctx, agent); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to copy the last of a restarted agent's conversation",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
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

// restartBrief is the restarted writer's first prompt, with the note it was restarted with.
func restartBrief(note string) string {
	if note = strings.TrimSpace(note); note != "" {
		return restartPrompt + "\n\n" + note
	}

	return restartPrompt
}

// freshStart is what a writer starting a new conversation is briefed with.
type freshStart struct {
	prompt           string
	task             *taskstore.Task
	workflow         *workflowstore.Workflow
	spawner          *agentstore.Agent
	earlierSessionID string
}

// plan is the writer's launch on its new conversation, briefed the way it was when it was made.
func (f *freshStart) plan(hn harness.Harness, agent agentstore.Agent) paneLaunch {
	launch := launchParams{
		Pane:         agent.Pane,
		Dir:          agent.Cwd,
		Flags:        agent.Flags,
		Prompt:       f.prompt,
		SessionID:    agent.SessionID,
		AgentID:      agent.ID,
		Role:         agent.Role,
		OwnsCheckout: true,
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
