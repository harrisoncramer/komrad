package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/internal/agenterr"
	"komrad/internal/fail"
	"komrad/internal/harness"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
)

var ErrAgentNotFailed = fail.Precondition("agent is not in a failed state")

type RetryCreateParams struct {
	ID      string
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

	marked, err := s.agents.MarkSettingUpIfFailedToCreate(ctx, params.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to mark %q for retry: %w", agent.Name, err)
	}
	if !marked {
		return nil, ErrAgentNotFailed.Saying("%q was changed while the retry was starting", agent.Name)
	}

	// The panel prefills its retry form with the flags already composed for the CLI the agent is
	// on, so flags that came back unchanged name nothing.
	flags := strings.TrimSpace(params.Flags)
	if flags == strings.TrimSpace(agent.Flags) {
		flags = ""
	}
	if flags != "" {
		agent.Flags = flags
	}
	if prompt := strings.TrimSpace(params.Prompt); prompt != "" {
		agent.Prompt = prompt
	}

	wf := workflowFor(ctx, s.workflows, *agent)
	provisioner, deprovisioner := worktreeScripts(wf, agent.ProvisionEnabled)
	hooks := hooksFor(wf, agent.ParentAgentID != nil, agent.ProvisionEnabled)

	workflowContext := ""
	if wf != nil {
		workflowContext = wf.Context
	}

	wanted := strings.TrimSpace(params.Harness)
	switched := wanted != "" && wanted != agent.Harness
	if wanted == "" {
		wanted = agent.Harness
	}

	hn, err := s.harnesses.GetByID(wanted)
	if err != nil {
		s.failCreate(ctx, *agent, err)
		return nil, err
	}

	if switched {
		agent.Harness = wanted
		if err := s.moveOntoHarness(ctx, agent, wf, flags, hn); err != nil {
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
	var parent *agentstore.Agent
	if agent.ParentAgentID != nil {
		if found, err := s.agents.GetByID(ctx, *agent.ParentAgentID); err == nil {
			parent = found
		}
	}

	placed, err := s.placeRetry(ctx, *agent)
	if err != nil {
		s.failCreate(ctx, *agent, err)
		return nil, err
	}
	if placed.openTurn != nil {
		defer placed.openTurn.Done()
	}

	claimsCheckout := isCheckoutClaimant(*agent, provisioner)
	if claimsCheckout {
		if err := s.claimCheckoutAgain(ctx, *agent); err != nil {
			s.failCreate(ctx, *agent, err)
			return nil, err
		}
	}

	agent.State = agentstore.StateSettingUp

	return s.startInPane(ctx, *agent, resolvedLaunch{
		harness:         hn,
		provisioner:     provisioner,
		deprovisioner:   deprovisioner,
		hooks:           hooks,
		defaultDir:      agent.Cwd,
		workflowContext: workflowContext,
		spawner:         spawner,
		parent:          parent,
		claimsCheckout:  claimsCheckout,
		workflow:        wf,
		task:            placed.task,
		openTurn:        placed.openTurn,
	})
}

// placeRetry reads the task a retried agent is on, taking a place in its hook queue when the retry opens it.
func (s *LaunchService) placeRetry(ctx context.Context, agent agentstore.Agent) (*placement, error) {
	if agent.TaskID == nil {
		return &placement{}, nil
	}

	task, err := s.tasks.GetTask(ctx, *agent.TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the task of agent %q: %w", agent.Name, err)
	}

	live, err := s.agents.ListLiveOnTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	return &placement{
		task:     task,
		openTurn: s.queueOpening(agent, live),
	}, nil
}

// claimCheckoutAgain claims the checkout of a retried agent's task again, refusing when another launch
// has claimed it since.
func (s *LaunchService) claimCheckoutAgain(ctx context.Context, agent agentstore.Agent) error {
	claimed, err := s.tasks.ClaimCheckoutAgain(ctx, *agent.TaskID)
	if err != nil {
		return fmt.Errorf("failed to claim the checkout of task %q again for agent %q: %w", *agent.TaskID, agent.Name, err)
	}
	if !claimed {
		return errTaskHasCheckout
	}

	return nil
}

// moveOntoHarness records the CLI a retry corrected an agent onto, with the flags and the
// conversation that belong to it.
func (s *LaunchService) moveOntoHarness(ctx context.Context, agent *agentstore.Agent, wf *workflowstore.Workflow, flags string, hn harness.Harness) error { //nolint:revive // a params struct is the follow-up
	workflowHarness, workflowFlags := s.roleDefaults(wf, agent.Role)

	agent.Flags = s.launchFlags(launchFlagsParams{
		harnessID:       agent.Harness,
		workflowHarness: workflowHarness,
		agentFlags:      flags,
		workflowFlags:   workflowFlags,
	})

	agent.SessionID = harness.MintSessionID(hn)

	return s.agents.SetHarness(ctx, agentstore.SetHarnessParams{
		ID:        agent.ID,
		Harness:   agent.Harness,
		Flags:     agent.Flags,
		SessionID: agent.SessionID,
	})
}
