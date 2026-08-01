package ui

import (
	"slices"
	"time"

	actionv1 "weave/proto/gen/action/v1"
	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
)

// The panel is written against the store types rather than the protos, so a listing is
// rendered into them on the way in. What the screens hold is the store's shape, which
// is what keeps the wire format out of them.

func protoToAgent(s *agentv1.Agent) agentstore.Agent {
	created, _ := time.Parse(time.RFC3339, s.GetCreatedAt())

	var archived *time.Time
	if parsed, err := time.Parse(time.RFC3339, s.GetArchivedAt()); err == nil {
		archived = &parsed
	}

	metadata := make([]agentstore.Metadata, 0, len(s.GetMetadata()))
	for _, entry := range s.GetMetadata() {
		metadata = append(metadata, agentstore.Metadata{Key: entry.GetKey(), Value: entry.GetValue()})
	}

	return agentstore.Agent{
		ID:         s.GetId(),
		Name:       s.GetName(),
		Pane:       s.GetPaneId(),
		Cwd:        s.GetCwd(),
		Flags:      s.GetFlags(),
		Prompt:     s.GetPrompt(),
		State:      s.GetState(),
		Harness:    s.GetHarness(),
		SessionID:  s.GetSessionId(),
		WorkflowID: s.GetWorkflowId(),
		ScheduleID: s.ScheduleId,
		CreatedAt:  created,
		ArchivedAt: archived,
		Mark:       s.Mark,
		Hidden:     s.GetHidden(),
		Metadata:   metadata,
	}
}

// actionItemsFrom renders listed actions into the items the screen is written against.
func actionItemsFrom(listed []*actionv1.Action) []actionItem {
	items := make([]actionItem, 0, len(listed))
	for _, action := range listed {
		items = append(items, actionItem{
			name:       action.GetName(),
			executable: action.GetExecutable(),
			preview:    action.GetPreview(),
			truncated:  action.GetPreviewTruncated(),
		})
	}

	return items
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
// against. The archive time comes with them, since the archive draws an archived folder
// differently from a live one.
func workflowsFrom(listed []*workflowv1.Workflow) []workflowstore.Workflow {
	workflows := make([]workflowstore.Workflow, 0, len(listed))
	for _, w := range listed {
		var archived *time.Time
		if parsed, err := time.Parse(time.RFC3339, w.GetArchivedAt()); err == nil {
			archived = &parsed
		}

		workflows = append(workflows, workflowstore.Workflow{
			ID:            w.GetId(),
			Name:          w.GetName(),
			Context:       w.GetContext(),
			Harness:       w.GetHarness(),
			Cwd:           w.GetCwd(),
			Flags:         w.GetFlags(),
			SetupAction:   w.GetSetupAction(),
			CleanupAction: w.GetCleanupAction(),
			ArchivedAt:    archived,
		})
	}

	return workflows
}

// archivedAgentsFrom renders listed agents into the store type the screen is
// written against, most recently archived first. Liveness is not carried: archiving
// kills the pane, so nothing in the archive is running.
//
// The order is applied here rather than asked of the service, whose pages are keyed
// on the id so a page resumes exactly where the last one ended. The panel reads the
// whole archive before it draws any of it, so sorting what came back is total, while
// paging the service by archive time would need a second key in every cursor to
// stay exact. A row archived before the column existed carries no time and sorts
// below the ones that do, by id, which is the order the listing already arrived in.
func archivedAgentsFrom(listed []*agentv1.Agent) []agentstore.Agent {
	agents := make([]agentstore.Agent, 0, len(listed))
	for _, agent := range listed {
		agents = append(agents, protoToAgent(agent))
	}

	archivedAt := func(agent agentstore.Agent) time.Time {
		if agent.ArchivedAt == nil {
			return time.Time{}
		}

		return *agent.ArchivedAt
	}

	slices.SortStableFunc(agents, func(a, b agentstore.Agent) int {
		return archivedAt(b).Compare(archivedAt(a))
	})

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
