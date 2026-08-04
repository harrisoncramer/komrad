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
	Prompt        string
	Title         string
	Harness       string
	Dir           string
	Flags         string
	SetupAction   *string
	CleanupAction *string
	WorkflowID    string
}

// createAgent launches an agent and returns it as the service now holds it, since the panel
// needs its id to focus the row the next listing brings in.
func (a api) createAgent(params createAgentParams) (*agentv1.Agent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	resp, err := a.agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:        params.Prompt,
		Title:         params.Title,
		Harness:       params.Harness,
		Dir:           params.Dir,
		Flags:         params.Flags,
		SetupAction:   params.SetupAction,
		CleanupAction: params.CleanupAction,
		WorkflowId:    params.WorkflowID,
	}))
	if err != nil {
		return nil, err
	}

	return resp.Msg.GetAgent(), nil
}

// archiveAgents stops agents and hides them, keeping the records and transcripts so search
// still reaches them.
func (a api) archiveAgents(ids []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))

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

// setMarks puts one mark on every agent given, and a nil mark clears the ones they carry.
func (a api) setMarks(ids []string, mark *string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.SetMarks(ctx, connect.NewRequest(&agentv1.SetMarksRequest{Ids: ids, Mark: mark}))

	return err
}

// restoreAgents takes agents out of the archive, which starts nothing. Resuming one is a
// call of its own.
func (a api) restoreAgents(ids []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	if _, err := a.agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids})); err != nil {
		return fmt.Errorf("failed to restore %d agents: %w", len(ids), err)
	}

	return nil
}

// resumeAgent puts an agent's conversation back in a pane and returns it as the service
// now holds it, since the stage shows the pane it came back in.
func (a api) resumeAgent(id string) (*agentv1.Agent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	resp, err := a.agents.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to resume agent %q: %w", id, err)
	}

	return resp.Msg.GetAgent(), nil
}

// deleteAgents destroys agents for good, keyed by id since a name identifies at most one
// live agent, which is what a deleted one is not.
func (a api) deleteAgents(ids []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: ids}))

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

// archiveSchedule hides a schedule, which stops it firing. The agents it launched are
// left alone.
func (a api) archiveSchedule(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.schedules.ArchiveSchedule(ctx, connect.NewRequest(&schedulev1.ArchiveScheduleRequest{Id: id}))

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

// restoreWorkflow takes a workflow out of the archive with the agents archived inside it,
// which starts nothing. Every one of them comes back the way a restored agent does.
func (a api) restoreWorkflow(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	if _, err := a.workflows.RestoreWorkflow(ctx, connect.NewRequest(&workflowv1.RestoreWorkflowRequest{Id: id})); err != nil {
		return fmt.Errorf("failed to restore workflow %q: %w", id, err)
	}

	return nil
}

// listActions is the user's actions as the service finds them, each with what its own header
// declares. The panel never reads the directory itself, so what it lists and what a model
// lists are one call.
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

// editAction opens an action's file in the user's editor, in the pane the panel has already
// opened under its stage. The service resolves the name to a path, since which directory an
// action was found in is not something the panel knows.
func (a api) editAction(agentID, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.actions.EditAction(ctx, connect.NewRequest(&actionv1.EditActionRequest{
		AgentId: agentID,
		Action:  name,
	}))

	return err
}
