package ui

import (
	"slices"
	"strings"
	"time"

	actionv1 "weave/proto/gen/action/v1"
	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"
)

// The panel is written against the store types rather than the protos, so a listing is
// rendered into them on the way in. What the screens hold is the store's shape, which
// is what keeps the wire format out of them.

func protoToAgent(s *agentv1.Agent) agentstore.Agent {
	created, _ := time.Parse(time.RFC3339, s.GetCreatedAt()) //nolint:errcheck // a timestamp that will not parse renders as the zero time

	var archived *time.Time
	if parsed, err := time.Parse(time.RFC3339, s.GetArchivedAt()); err == nil {
		archived = &parsed
	}

	metadata := xslice.Map(s.GetMetadata(), func(entry *agentv1.AgentMetadata) agentstore.Metadata {
		return agentstore.Metadata{Key: entry.GetKey(), Value: entry.GetValue()}
	})

	return agentstore.Agent{
		ID:              s.GetId(),
		Name:            s.GetName(),
		Pane:            s.GetPaneId(),
		Cwd:             s.GetCwd(),
		Flags:           s.GetFlags(),
		Prompt:          s.GetPrompt(),
		State:           s.GetState(),
		Harness:         s.GetHarness(),
		SessionID:       s.GetSessionId(),
		WorkflowID:      s.GetWorkflowId(),
		ScheduleID:      s.ScheduleId,
		CreatedAt:       created,
		ArchivedAt:      archived,
		Tag:             s.Tag,
		Hidden:          s.GetHidden(),
		Metadata:        metadata,
		Branch:          s.Branch,
		WorktreeRemoved: s.GetWorktreeRemoved(),
	}
}

// isInternalAction says a file in the actions directory is shell the user keeps for other
// actions to reuse rather than an action itself. An underscore in front of the name is how
// they say so, and nothing the panel offers to pick carries one.
func isInternalAction(name string) bool {
	return strings.HasPrefix(name, "_")
}

// actionItemsFrom renders listed actions into the items the screen is written against,
// leaving out the ones written to be sourced rather than run.
func actionItemsFrom(listed []*actionv1.Action) []actionItem {
	external := xslice.Filter(listed, func(action *actionv1.Action) bool {
		return !isInternalAction(action.GetName())
	})

	return xslice.Map(external, func(action *actionv1.Action) actionItem {
		return actionItem{
			name:        action.GetName(),
			executable:  action.GetExecutable(),
			description: action.GetDescription(),
			timeout:     action.GetTimeout().AsDuration(),
			problems:    action.GetHeaderProblems(),
			text:        action.GetText(),
		}
	})
}

func agentItemsFrom(listed []*agentv1.Agent) []agentItem {
	return xslice.Map(listed, func(agent *agentv1.Agent) agentItem {
		return agentItem{agent: protoToAgent(agent), alive: agent.GetAlive(), unreadMail: agent.GetUnreadMail()}
	})
}

// workflowsFrom renders listed workflows into the store type the tree is written
// against. The archive time comes with them, since the archive draws an archived folder
// differently from a live one.
func workflowsFrom(listed []*workflowv1.Workflow) []workflowstore.Workflow {
	return xslice.Map(listed, func(w *workflowv1.Workflow) workflowstore.Workflow {
		var archived *time.Time
		if parsed, err := time.Parse(time.RFC3339, w.GetArchivedAt()); err == nil {
			archived = &parsed
		}

		return workflowstore.Workflow{
			ID:            w.GetId(),
			Name:          w.GetName(),
			Context:       w.GetContext(),
			Harness:       w.GetHarness(),
			Cwd:           w.GetCwd(),
			Flags:         w.GetFlags(),
			SetupAction:   w.GetSetupAction(),
			CleanupAction: w.GetCleanupAction(),
			Worktree:      worktreeConfigFromProto(w.GetWorktree()),
			ArchivedAt:    archived,
		}
	})
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
	agents := xslice.Map(listed, protoToAgent)

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
	return xslice.Map(listed, func(s *schedulev1.Schedule) scheduleItem {
		item := scheduleItem{schedule: scheduleFromProto(s)}
		if next := s.GetNextRunAt(); next != nil {
			item.nextRun = next.AsTime().Local()
		}

		return item
	})
}

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
