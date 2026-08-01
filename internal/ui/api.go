package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	actionv1 "weave/proto/gen/action/v1"
	"weave/proto/gen/action/v1/actionv1connect"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	schedulev1 "weave/proto/gen/schedule/v1"
	schedulev1connect "weave/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// requestTimeout bounds a single call made in response to a keypress. It is generous
// because creating an agent opens a tmux window on the far side, and exists so a wedged
// server leaves the panel usable rather than frozen.
const requestTimeout = 10 * time.Second

// api holds the service clients the panel acts through. Every change goes over these, the
// same way the MCP tools and the CLI reach them, so what a person may do from the panel
// and what a model may do cannot drift apart. Each call below is one RPC under its own
// deadline, and all but the two reads an edit form opens on run off the render loop.
type api struct {
	agents    agentv1connect.AgentServiceClient
	workflows workflowv1connect.WorkflowServiceClient
	schedules schedulev1connect.ScheduleServiceClient
	actions   actionv1connect.ActionServiceClient
}

// renameAgent renames an agent by id. A blank or unchanged name is a no-op, so the form's
// empty submit asks the service for nothing.
func (a api) renameAgent(agent agentstore.Agent, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == agent.Name {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
		Id:   agent.ID,
		Name: name,
	}))

	return err
}

// createAgentParams is a launch: the workflow's blueprint as the form left it, plus the
// workflow the agent lands in.
type createAgentParams struct {
	Prompt     string
	Title      string
	Dir        string
	Flags      string
	WorkflowID string
}

// createAgent launches an agent.
func (a api) createAgent(params createAgentParams) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     params.Prompt,
		Title:      params.Title,
		Dir:        params.Dir,
		Flags:      params.Flags,
		WorkflowId: params.WorkflowID,
	}))

	return err
}

// archiveAgent stops an agent and hides it, keeping the record and transcript so search
// still reaches it.
func (a api) archiveAgent(agent agentstore.Agent) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.ArchiveAgent(ctx, connect.NewRequest(&agentv1.ArchiveAgentRequest{Name: agent.Name}))

	return err
}

// setHidden takes an agent out of the list or puts it back. It keeps running and every
// other view still holds it.
func (a api) setHidden(agent agentstore.Agent, hidden bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{Id: agent.ID, Hidden: hidden}))

	return err
}

// setMark puts a mark on an agent, and a nil mark clears the one it carries.
func (a api) setMark(agent agentstore.Agent, mark *string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{Id: agent.ID, Mark: mark}))

	return err
}

// restoreAgent brings an agent back and returns it as the service now holds it.
func (a api) restoreAgent(id string) (*agentv1.Agent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	resp, err := a.agents.RestoreAgent(ctx, connect.NewRequest(&agentv1.RestoreAgentRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to restore agent %q: %w", id, err)
	}

	return resp.Msg.GetAgent(), nil
}

// deleteAgent destroys an agent for good, keyed by id since a name identifies at most one
// live agent, which is what a deleted one is not.
func (a api) deleteAgent(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.DeleteAgent(ctx, connect.NewRequest(&agentv1.DeleteAgentRequest{Id: id}))

	return err
}

// getSchedule refetches one schedule, so an edit form opens on what is stored rather than
// the row the listing carried. A form has nothing to open on until the answer is in, so
// this is the one call left on the render loop and takes a listing's short deadline.
func (a api) getSchedule(id string) (*schedulestore.Schedule, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
	defer cancel()

	resp, err := a.schedules.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
	}

	stored := scheduleFromProto(resp.Msg.GetSchedule())

	return &stored, nil
}

// createSchedule records a new schedule.
func (a api) createSchedule(cfg schedulestore.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.schedules.CreateSchedule(ctx, connect.NewRequest(&schedulev1.CreateScheduleRequest{
		Name:       strings.TrimSpace(cfg.Name),
		Cron:       cfg.Cron,
		Timezone:   cfg.Timezone,
		Enabled:    cfg.Enabled,
		HideAgents: cfg.HideAgents,
		Prompt:     cfg.Prompt,
		WorkflowId: cfg.WorkflowID,
	}))

	return err
}

// updateSchedule writes every field back, since the service replaces rather than patches.
func (a api) updateSchedule(id string, cfg schedulestore.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.schedules.UpdateSchedule(ctx, connect.NewRequest(&schedulev1.UpdateScheduleRequest{
		Id:         id,
		Name:       strings.TrimSpace(cfg.Name),
		Cron:       cfg.Cron,
		Timezone:   cfg.Timezone,
		Enabled:    cfg.Enabled,
		HideAgents: cfg.HideAgents,
		Prompt:     cfg.Prompt,
		WorkflowId: cfg.WorkflowID,
	}))

	return err
}

// setScheduleEnabled pauses or resumes a schedule without touching the rest of its record.
func (a api) setScheduleEnabled(id string, enabled bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.schedules.SetScheduleEnabled(ctx, connect.NewRequest(&schedulev1.SetScheduleEnabledRequest{
		Id:      id,
		Enabled: enabled,
	}))

	return err
}

// deleteSchedule removes a schedule. The agents it launched are left alone.
func (a api) deleteSchedule(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.schedules.DeleteSchedule(ctx, connect.NewRequest(&schedulev1.DeleteScheduleRequest{Id: id}))

	return err
}

// getWorkflow refetches one workflow, so an edit form opens on what is stored rather than
// the row the listing carried. It reads on the render loop for the reason getSchedule
// does, and takes the same short deadline.
func (a api) getWorkflow(id string) (*workflowstore.Workflow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
	defer cancel()

	resp, err := a.workflows.GetWorkflow(ctx, connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	w := resp.Msg.GetWorkflow()

	return &workflowstore.Workflow{
		ID:            w.GetId(),
		Name:          w.GetName(),
		Context:       w.GetContext(),
		Harness:       w.GetHarness(),
		Cwd:           w.GetCwd(),
		Flags:         w.GetFlags(),
		SetupAction:   w.GetSetupAction(),
		CleanupAction: w.GetCleanupAction(),
	}, nil
}

// createWorkflow adds a workflow, defaulting a blank name so the list always has a label
// to render.
func (a api) createWorkflow(cfg workflowstore.Config) error {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "workflow"
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:          name,
		Context:       cfg.Context,
		Harness:       cfg.Harness,
		Cwd:           cfg.Cwd,
		Flags:         cfg.Flags,
		SetupAction:   cfg.SetupAction,
		CleanupAction: cfg.CleanupAction,
	}))

	return err
}

// updateWorkflow writes every field back, since the service replaces rather than patches.
func (a api) updateWorkflow(w workflowstore.Workflow) error {
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.workflows.UpdateWorkflow(ctx, connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
		Id:            w.ID,
		Name:          name,
		Context:       w.Context,
		Harness:       w.Harness,
		Cwd:           w.Cwd,
		Flags:         w.Flags,
		SetupAction:   w.SetupAction,
		CleanupAction: w.CleanupAction,
	}))

	return err
}

// archiveWorkflow hides a workflow, archiving the agents inside it. The service refuses
// while any schedule still launches into it.
func (a api) archiveWorkflow(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.workflows.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: id}))

	return err
}

// listActions is the user's actions as the service finds them, each with the start of its
// own text for the preview. The panel never reads the directory itself, so what it lists
// and what a model lists are one call.
func (a api) listActions() ([]*actionv1.Action, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	resp, err := a.actions.ListActions(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		return nil, err
	}

	return resp.Msg.GetActions(), nil
}

// runAction fires an action on an agent, returning once the service has started the
// process, so an error is always from before it began rather than from what it did.
func (a api) runAction(agentID, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.actions.RunAction(ctx, connect.NewRequest(&actionv1.RunActionRequest{
		AgentId: agentID,
		Action:  name,
	}))

	return err
}
