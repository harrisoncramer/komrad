package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
			return nil, ErrAgentNotFound.Saying("no agent with id %q", params.ID)
		}

		return nil, err
	}
	if agent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q is archived", ErrAgentArchived, agent.Name)
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

	provisioner := provisionerFor(ctx, s.workflows, *agent)
	deprovisioner := deprovisionerFor(ctx, s.workflows, *agent)
	hooks := agentHooks(ctx, s.workflows, *agent)

	wf := workflowFor(ctx, s.workflows, *agent)
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
	})
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

	agent.SessionID = hn.MintSessionID()

	return s.agents.SetHarness(ctx, agentstore.SetHarnessParams{
		ID:        agent.ID,
		Harness:   agent.Harness,
		Flags:     agent.Flags,
		SessionID: agent.SessionID,
	})
}
