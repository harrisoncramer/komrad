package ui

import (
	"slices"
	"time"

	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"
)

func protoToAgent(s *agentv1.Agent) agentstore.Agent {
	created, _ := time.Parse(time.RFC3339, s.GetCreatedAt())           //nolint:errcheck // a timestamp that will not parse renders as the zero time
	stateChanged, _ := time.Parse(time.RFC3339, s.GetStateChangedAt()) //nolint:errcheck // a timestamp that will not parse renders as the zero time

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
		State:           agentstore.State(s.GetState()),
		Harness:         s.GetHarness(),
		SessionID:       s.GetSessionId(),
		WorkflowID:      s.GetWorkflowId(),
		ScheduleID:      s.ScheduleId,
		CreatedAt:       created,
		StateChangedAt:  stateChanged,
		ArchivedAt:      archived,
		Tag:             s.Tag,
		Hidden:          s.GetHidden(),
		Metadata:        metadata,
		Branch:          s.Branch,
		WorktreeRemoved: s.GetWorktreeRemoved(),
		ParentAgentID:   s.ParentAgentId,
	}
}

func agentItemsFrom(listed []*agentv1.Agent) []agentItem {
	return xslice.Map(listed, func(agent *agentv1.Agent) agentItem {
		return agentItem{
			agent:        protoToAgent(agent),
			alive:        agent.GetAlive(),
			unreadMail:   agent.GetUnreadMail(),
			unviewed:     agent.GetUnviewed(),
			failedRun:    agent.GetFailedRun(),
			failedRunLog: agent.FailedRunLog,
			recentTokens: tokenUsageFrom(agent.GetRecentTokens()),
		}
	})
}

func workflowsFrom(listed []*workflowv1.Workflow) []workflowstore.Workflow {
	return xslice.Map(listed, workflowFrom)
}

func workflowFrom(w *workflowv1.Workflow) workflowstore.Workflow {
	var archived *time.Time
	if parsed, err := time.Parse(time.RFC3339, w.GetArchivedAt()); err == nil {
		archived = &parsed
	}

	return workflowstore.Workflow{
		ID:       w.GetId(),
		Name:     w.GetName(),
		Context:  w.GetContext(),
		Harness:  w.GetHarness(),
		Cwd:      w.GetCwd(),
		Flags:    w.GetFlags(),
		Worktree: worktreeConfigFromProto(w.GetWorktree()),
		Hooks: xslice.Map(w.GetHooks(), func(hook *workflowv1.WorkflowHook) workflowstore.Hook {
			return workflowstore.Hook{Event: hook.GetEvent(), Script: hook.GetScript()}
		}),
		ArchiveOnPRClosed: w.GetArchiveOnPrClosed(),
		ArchivedAt:        archived,
	}
}

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
	schedule := schedulestore.Schedule{
		ID:                s.GetId(),
		Name:              s.GetName(),
		Cron:              s.GetCron(),
		Timezone:          s.GetTimezone(),
		Enabled:           s.GetEnabled(),
		HideAgents:        s.GetHideAgents(),
		Prompt:            s.GetPrompt(),
		WorkflowID:        s.GetWorkflowId(),
		LastResultStatus:  schedulestore.LaunchResultStatus(s.GetLastResultStatus()),
		LastResultMessage: s.GetLastResultMessage(),
	}

	if at := s.GetLastResultAt(); at != nil {
		lastResultAt := at.AsTime().Local()
		schedule.LastResultAt = &lastResultAt
	}

	return schedule
}
