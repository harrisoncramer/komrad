package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/internal/agenterr"
	"komrad/internal/fail"
	"komrad/internal/harness"
	"komrad/internal/scriptrun"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"

	"github.com/google/uuid"
)

var ErrAgentNotFailed = fail.Precondition("agent is not in a failed state")

type RetryCreateParams struct {
	ID      uuid.UUID
	Flags   string
	Prompt  string
	Harness string
}

// RetryCreate relaunches an agent that failed to create, optionally with corrected flags.
func (s *LaunchService) RetryCreate(ctx context.Context, params RetryCreateParams) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, params.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, agenterr.ErrAgentNotFound.Saying("no agent with id %q", params.ID)
		}

		return nil, err
	}
	if agent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q is archived", agenterr.ErrAgentArchived, agent.Name)
	}
	if agent.State != agentstore.StateFailedToCreate {
		return nil, ErrAgentNotFailed.Saying("%q is %s, not failed_to_create", agent.Name, agent.State)
	}

	if err := requireRoomForAgents(ctx, s.agents, s.usage.MaxAgents, 1); err != nil {
		return nil, err
	}

	wf := workflowFor(ctx, s.workflows, *agent)
	provisioner, deprovisioner := scriptrun.WorktreeScripts(wf, agent.ProvisionEnabled)
	hooks := hooksFor(wf, agent.Role, agent.ProvisionEnabled)

	task, err := s.markRetrying(ctx, *agent)
	if err != nil {
		return nil, err
	}

	if prompt := strings.TrimSpace(params.Prompt); prompt != "" {
		agent.Prompt = prompt
	}

	wanted := wantedHarness(*agent, params.Harness)
	hn, err := s.harnesses.GetByID(wanted)
	if err != nil {
		s.failCreate(ctx, *agent, err)
		return nil, err
	}

	if s.moveOnto(moveOntoParams{
		agent:    agent,
		workflow: wf,
		harness:  wanted,
		flags:    params.Flags,
	}) {
		agent.SessionID = harness.MintSessionID(hn)
		if err := s.agents.SetHarness(ctx, agentstore.SetHarnessParams{
			ID:        agent.ID,
			Harness:   agent.Harness,
			Flags:     agent.Flags,
			SessionID: agent.SessionID,
		}); err != nil {
			s.failCreate(ctx, *agent, err)
			return nil, err
		}
	}

	var spawner *agentstore.Agent
	if agent.SpawnedByAgentID != nil {
		if found, err := s.agents.GetByID(ctx, *agent.SpawnedByAgentID); err == nil {
			spawner = found
		}
	}
	placed, err := s.placeRetry(ctx, *agent, task)
	if err != nil {
		s.failCreate(ctx, *agent, err)
		return nil, err
	}
	if placed.openTurn != nil {
		defer placed.openTurn.Done()
	}

	claimsCheckout := isCheckoutClaimant(*agent, provisioner)
	if claimsCheckout {
		if err := s.tasks.ClaimCheckoutAgain(ctx, *agent); err != nil {
			s.failCreate(ctx, *agent, err)
			return nil, err
		}
	}

	agent.State = agentstore.StateSettingUp

	return s.startInPane(ctx, *agent, resolvedLaunch{
		harness:        hn,
		provisioner:    provisioner,
		deprovisioner:  deprovisioner,
		hooks:          hooks,
		defaultDir:     agent.Cwd,
		spawner:        spawner,
		claimsCheckout: claimsCheckout,
		workflow:       wf,
		task:           placed.task,
		openTurn:       placed.openTurn,
	})
}

// placeRetry takes a retried agent a place in its task's hook queue when the retry opens it.
func (s *LaunchService) placeRetry(ctx context.Context, agent agentstore.Agent, task *taskstore.Task) (*placement, error) {
	if task == nil {
		return &placement{}, nil
	}

	live, err := s.agents.ListLiveOnTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	return &placement{
		task:     task,
		openTurn: s.tasks.QueueOpening(agent, live),
	}, nil
}

// wantedHarness is the CLI a relaunch asked for, or the one the agent runs when it named none.
func wantedHarness(agent agentstore.Agent, asked string) string {
	if asked = strings.TrimSpace(asked); asked != "" {
		return asked
	}

	return agent.Harness
}

type moveOntoParams struct {
	agent    *agentstore.Agent
	workflow *workflowstore.Workflow
	harness  string
	flags    string
}

// moveOnto puts on an agent the CLI and flags a relaunch asked for, and reports whether its CLI
// changed.
func (s *LaunchService) moveOnto(params moveOntoParams) bool {
	agent := params.agent

	// The panel prefills its retry and restart forms with the flags already composed for the CLI
	// the agent is on, so flags that came back unchanged name nothing.
	flags := strings.TrimSpace(params.flags)
	if flags == strings.TrimSpace(agent.Flags) {
		flags = ""
	}

	if params.harness == agent.Harness {
		if flags != "" {
			agent.Flags = flags
		}

		return false
	}

	agent.Harness = params.harness
	workflowHarness, workflowFlags := s.roleDefaults(params.workflow, agent.Role)
	agent.Flags = s.launchFlags(launchFlagsParams{
		harnessID:       agent.Harness,
		workflowHarness: workflowHarness,
		agentFlags:      flags,
		workflowFlags:   workflowFlags,
	})

	return true
}

// markRetrying marks a failed agent for its retry, returning the task it is on.
func (s *LaunchService) markRetrying(ctx context.Context, agent agentstore.Agent) (*taskstore.Task, error) {
	task, err := s.tasks.GetAgentTask(ctx, agent)
	if err != nil && !errors.Is(err, taskstore.ErrNoTask) {
		return nil, fmt.Errorf("failed to read the task of agent %q: %w", agent.Name, err)
	}

	ok, err := s.agents.MarkSettingUpIfFailedToCreate(ctx, agent.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to mark %q for retry: %w", agent.Name, err)
	}
	if !ok {
		return nil, ErrAgentNotFailed.Saying("%q was changed while the retry was starting", agent.Name)
	}

	return task, nil
}
