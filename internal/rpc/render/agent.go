package render

import (
	"time"

	"komrad/lib/dates"
	"komrad/lib/xptr"
	"komrad/lib/xstr"

	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
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
		Id:                 stored.ID,
		Name:               stored.Name,
		Cwd:                stored.Cwd,
		Flags:              stored.Flags,
		Prompt:             prompt,
		PromptTruncated:    promptTruncated,
		PaneId:             stored.Pane,
		State:              AgentStateOf(stored.State),
		Role:               AgentRoleOf(stored.Role),
		SessionId:          stored.SessionID,
		Harness:            stored.Harness,
		WorkflowId:         stored.WorkflowID,
		ScheduleId:         stored.ScheduleID,
		CreatedAt:          dates.Stamp(stored.CreatedAt),
		StateChangedAt:     dates.Stamp(stored.StateChangedAt),
		ReportedReason:     stored.ReportedReason,
		FailureReason:      stored.FailureReason,
		ArchivedAt:         dates.StampPtr(stored.ArchivedAt),
		Alive:              xptr.Ptr(stored.Alive),
		Hidden:             stored.Hidden,
		IdempotencyKey:     stored.IdempotencyKey,
		SpawnedByAgentId:   stored.SpawnedByAgentID,
		ParentAgentId:      stored.ParentAgentID,
		Branch:             stored.Branch,
		RepositoryUrl:      stored.RepositoryURL,
		HeadSha:            stored.HeadSHA,
		WorktreeRemoved:    stored.WorktreeRemoved,
		SequenceInProgress: stored.SequenceInProgress,
		FailedRun:          row.FailedRun,
		FailedRunLog:       row.FailedRunLog,
		Documents:          row.Documents,
		DocumentsUpdatedAt: dates.PreciseStampPtr(row.DocumentsUpdatedAt),
		MovedSinceRound:    stored.MovedSinceRound,
		HeldAt:             heldStamp(stored.HeldAt),
	}
}

// WatchedAgent draws an agent as the watch carries it, an archived one with only the opening of its prompt.
func WatchedAgent(row rowstore.Row) *agentv1.Agent {
	if row.ArchivedAt != nil {
		return Agent(row, AgentOptions{PromptHead: SummaryHead})
	}

	return Agent(row, AgentOptions{})
}

// ReviewVerdictOf is a verdict as the wire carries it.
func ReviewVerdictOf(verdict reviewstore.Verdict) agentv1.ReviewVerdict {
	switch verdict {
	case reviewstore.VerdictApproved:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED
	case reviewstore.VerdictChangesRequested:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED
	case reviewstore.VerdictCommented:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED
	default:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED
	}
}

// heldStamp is when a person took the agent's worktree, empty while nobody has.
func heldStamp(at *time.Time) string {
	if at == nil {
		return ""
	}

	return dates.Stamp(*at)
}

// AgentStateOf is a stored state as the wire carries it.
func AgentStateOf(state agentstore.State) agentv1.AgentState {
	switch state {
	case agentstore.StateIdle:
		return agentv1.AgentState_AGENT_STATE_IDLE
	case agentstore.StateThinking:
		return agentv1.AgentState_AGENT_STATE_THINKING
	case agentstore.StateWaiting:
		return agentv1.AgentState_AGENT_STATE_WAITING
	case agentstore.StateSettingUp:
		return agentv1.AgentState_AGENT_STATE_SETTING_UP
	case agentstore.StateFailedToCreate:
		return agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
	case agentstore.StateFailedToCleanup:
		return agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
	case agentstore.StateUncertain:
		return agentv1.AgentState_AGENT_STATE_UNCERTAIN
	}

	return agentv1.AgentState_AGENT_STATE_UNSPECIFIED
}

// AgentRoleOf is a stored role as the wire carries it.
func AgentRoleOf(role agentstore.Role) agentv1.AgentRole {
	switch role {
	case agentstore.RoleImplementer:
		return agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER
	case agentstore.RoleReviewer:
		return agentv1.AgentRole_AGENT_ROLE_REVIEWER
	case agentstore.RoleOrchestrator:
		return agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR
	}

	return agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED
}
