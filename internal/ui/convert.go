package ui

import (
	"time"

	agentv1 "claude-agents/proto/gen/agent/v1"
	schedulev1 "claude-agents/proto/gen/schedule/v1"
	workflowv1 "claude-agents/proto/gen/workflow/v1"

	agentstore "claude-agents/internal/store/agent"
	schedulestore "claude-agents/internal/store/schedule"
	workflowstore "claude-agents/internal/store/workflow"
)

// The panel is written against the store types rather than the protos, so a listing
// is rendered into them on the way in. Nothing downstream of here holds a proto,
// which is what keeps the wire shape out of the screens.

func protoToAgent(s *agentv1.Agent) agentstore.Agent {
	created, _ := time.Parse(time.RFC3339, s.GetCreatedAt())

	metadata := make([]agentstore.Metadata, 0, len(s.GetMetadata()))
	for _, entry := range s.GetMetadata() {
		metadata = append(metadata, agentstore.Metadata{Key: entry.GetKey(), Value: entry.GetValue()})
	}

	return agentstore.Agent{
		ID:              s.GetId(),
		Name:            s.GetName(),
		Pane:            s.GetPaneId(),
		Cwd:             s.GetCwd(),
		Flags:           s.GetFlags(),
		Prompt:          s.GetPrompt(),
		State:           s.GetState(),
		ClaudeSessionID: s.GetClaudeSessionId(),
		WorkflowID:      s.GetWorkflowId(),
		ScheduleID:      s.ScheduleId,
		CreatedAt:       created,
		Mark:            s.Mark,
		Hidden:          s.GetHidden(),
		Metadata:        metadata,
	}
}

// agentItemsFrom maps listed agents to agent items, marking which panes are live.
func agentItemsFrom(listed []*agentv1.Agent) []agentItem {
	items := make([]agentItem, 0, len(listed))
	for _, agent := range listed {
		items = append(items, agentItem{agent: protoToAgent(agent), alive: agent.GetAlive()})
	}

	return items
}

// workflowsFrom renders listed workflows into the store type the tree is written
// against.
func workflowsFrom(listed []*workflowv1.Workflow) []workflowstore.Workflow {
	workflows := make([]workflowstore.Workflow, 0, len(listed))
	for _, w := range listed {
		workflows = append(workflows, workflowstore.Workflow{
			ID:      w.GetId(),
			Name:    w.GetName(),
			Context: w.GetContext(),
			Cwd:     w.GetCwd(),
			Flags:   w.GetFlags(),
		})
	}

	return workflows
}

// deletedAgentsFrom renders listed agents into the store type the screen is
// written against. Liveness is not carried: archiving kills the pane, so nothing
// in the trash is running.
func deletedAgentsFrom(listed []*agentv1.Agent) []agentstore.Agent {
	agents := make([]agentstore.Agent, 0, len(listed))
	for _, agent := range listed {
		agents = append(agents, protoToAgent(agent))
	}

	return agents
}

// scheduleItemsFrom maps listed schedules into the items the list is written
// against, carrying the next run the service derived.
func scheduleItemsFrom(listed []*schedulev1.Schedule) []scheduleItem {
	items := make([]scheduleItem, 0, len(listed))
	for _, s := range listed {
		item := scheduleItem{schedule: scheduleFromProto(s)}
		if next := s.GetNextRunAt(); next != nil {
			item.nextRun = next.AsTime().Local()
		}
		items = append(items, item)
	}

	return items
}

// scheduleFromProto renders a listed schedule into the store type the screen is
// written against.
func scheduleFromProto(s *schedulev1.Schedule) schedulestore.Schedule {
	return schedulestore.Schedule{
		ID:         s.GetId(),
		Name:       s.GetName(),
		Cron:       s.GetCron(),
		Timezone:   s.GetTimezone(),
		Enabled:    s.GetEnabled(),
		HideAgents: s.GetHideAgents(),
		Prompt:     s.GetPrompt(),
		WorkflowID: s.GetWorkflowId(),
	}
}
