package render

import (
	agentstore "komrad/internal/store/agent"
	rowstore "komrad/internal/store/row"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	"komrad/lib/xstr"
	agentv1 "komrad/proto/gen/agent/v1"
)

// AgentOptions is how much of an agent is drawn.
type AgentOptions struct {
	// PromptHead draws the opening of the prompt rather than the whole of it when set.
	PromptHead int
}

// Agent draws one agent's row as the wire carries it.
func Agent(row rowstore.Row, options AgentOptions) *agentv1.Agent {
	stored := row.Agent
	prompt, promptTruncated := stored.Prompt, false
	if options.PromptHead > 0 {
		prompt, promptTruncated = xstr.Head(stored.Prompt, options.PromptHead)
	}

	return &agentv1.Agent{
		Id:                 stored.ID.String(),
		Name:               stored.Name,
		Cwd:                stored.Cwd,
		Flags:              stored.Flags,
		Prompt:             prompt,
		PromptTruncated:    promptTruncated,
		PaneId:             stored.Pane,
		State:              stored.State,
		Role:               stored.Role,
		SessionId:          stored.SessionID,
		Harness:            stored.Harness,
		WorkflowId:         stored.WorkflowID.String(),
		ScheduleId:         formatOptionalID(stored.ScheduleID),
		CreatedAt:          dates.Stamp(stored.CreatedAt),
		StateChangedAt:     dates.Stamp(stored.StateChangedAt),
		ReportedReason:     stored.ReportedReason,
		FailureReason:      stored.FailureReason,
		TurnError:          readTurnError(stored),
		ArchivedAt:         dates.StampPtr(stored.ArchivedAt),
		Alive:              xptr.Ptr(stored.Alive),
		IdempotencyKey:     stored.IdempotencyKey,
		SpawnedByAgentId:   formatOptionalID(stored.SpawnedByAgentID),
		FailedRun:          row.FailedRun,
		FailedRunLog:       row.FailedRunLog,
		Documents:          row.Documents,
		DocumentsUpdatedAt: dates.StampPtr(row.DocumentsUpdatedAt),
		TaskId:             formatOptionalID(stored.TaskID),
		TaskSlug:           row.TaskSlug,
		ProvisionDisabled:  stored.IsProvisionerDisabled(),
		Hooks:              stored.Hooks,
	}
}

// WatchedAgent draws an agent as the watch carries it, an archived one with only the opening of its prompt.
func WatchedAgent(row rowstore.Row) *agentv1.Agent {
	if row.ArchivedAt != nil {
		return Agent(row, AgentOptions{PromptHead: SummaryHead})
	}

	return Agent(row, AgentOptions{})
}

func readTurnError(stored agentstore.Agent) string {
	if !stored.Turn.IsFailed() {
		return ""
	}

	return stored.Turn.Error
}
