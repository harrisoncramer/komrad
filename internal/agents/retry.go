package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/internal/fail"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
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

	if err := requireRoomForAgents(ctx, s.agents, s.maxAgents, 1); err != nil {
		return nil, err
	}

	marked, err := s.agents.MarkSettingUpIfFailedToCreate(ctx, params.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to mark %q for retry: %w", agent.Name, err)
	}
	if !marked {
		return nil, ErrAgentNotFailed.Saying("%q was changed while the retry was starting", agent.Name)
	}

	if flags := strings.TrimSpace(params.Flags); flags != "" {
		agent.Flags = flags
	}
	if prompt := strings.TrimSpace(params.Prompt); prompt != "" {
		agent.Prompt = prompt
	}
	if harness := strings.TrimSpace(params.Harness); harness != "" {
		agent.Harness = harness
	}

	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		s.failCreate(ctx, *agent, err)
		return nil, err
	}

	provisioner := provisionerFor(ctx, s.workflows, *agent)
	deprovisioner := deprovisionerFor(ctx, s.workflows, *agent)
	hooks := agentHooks(ctx, s.workflows, *agent)

	wf := workflowFor(ctx, s.workflows, *agent)
	workflowContext := ""
	if wf != nil {
		workflowContext = wf.Context
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
