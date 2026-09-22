package render

import (
	"time"

	"weave/internal/git"
	"weave/lib/dates"
	"weave/lib/xptr"
	"weave/lib/xslice"
	"weave/lib/xstr"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	rowstore "weave/internal/store/row"
	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
)

// AgentOptions is how much of an agent is drawn.
type AgentOptions struct {
	// PromptHead draws the opening of the prompt rather than the whole of it when set.
	PromptHead int
}

// Agent draws one agent's row as the wire carries it.
func Agent(row rowstore.Row, options AgentOptions) *agentv1.Agent {
	stored := row.Agent
	metadata := xslice.Map(stored.Metadata, func(entry agentstore.Metadata) *agentv1.AgentMetadata {
		return &agentv1.AgentMetadata{Key: entry.Key, Value: entry.Value}
	})

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
		ArchivedAt:         dates.StampPtr(stored.ArchivedAt),
		Alive:              xptr.Ptr(stored.Alive),
		UnreadMail:         row.UnreadMail,
		Mark:               row.Mark,
		Hidden:             stored.Hidden,
		Metadata:           metadata,
		IdempotencyKey:     stored.IdempotencyKey,
		SpawnedByAgentId:   stored.SpawnedByAgentID,
		ParentAgentId:      stored.ParentAgentID,
		Branch:             stored.Branch,
		RepositoryUrl:      stored.RepositoryURL,
		WorktreeRemoved:    stored.WorktreeRemoved,
		SequenceInProgress: stored.SequenceInProgress,
		FailedRun:          row.FailedRun,
		FailedRunLog:       row.FailedRunLog,
		LocalReviewRounds:  row.LocalRounds,
		LocalReviewVerdict: localVerdictOf(row.LocalVerdict),
		LocalReviewTree:    standingOf(localTreeOf(row)),
		HeldAt:             heldStamp(stored.HeldAt),
		PullRequest:        pullRequestStandingOf(row),
	}
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

// localVerdictOf is the last local conclusion as the wire carries it, UNSPECIFIED when there is
// none.
func localVerdictOf(verdict *reviewstore.Verdict) agentv1.ReviewVerdict {
	if verdict == nil {
		return agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED
	}

	return ReviewVerdictOf(*verdict)
}

// pullRequestStandingOf is where the agent's pull request stands as the wire carries it:
// unread until the sync has looked at the repository, none when it has and found nothing on
// the branch, else the current one and its state.
func pullRequestStandingOf(row rowstore.Row) *agentv1.PullRequestStanding {
	if row.PullRequest == nil {
		state := pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD
		if row.RepositoryRead {
			state = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE
		}

		return &agentv1.PullRequestStanding{State: state}
	}

	return &agentv1.PullRequestStanding{
		State:        PullRequestStateOf(*row.PullRequest),
		Number:       xptr.Ptr(int32(row.PullRequest.Number)),
		Url:          xptr.Ptr(row.PullRequest.URL),
		Title:        xptr.Ptr(row.PullRequest.Title),
		Checks:       ChecksOf(row.PullRequest.Checks),
		Mergeability: MergeabilityOf(row.PullRequest.Mergeability),
	}
}

// localTreeOf is where the worktree stands against the round the local verdict was given on,
// unmoved while the standing was measured against any other round.
func localTreeOf(row rowstore.Row) git.Standing {
	if row.LocalSnapshotSHA == "" || row.TreeStandingSHA != row.LocalSnapshotSHA {
		return git.StandingSame
	}

	return row.TreeStanding
}

// standingOf is a standing as the wire carries it.
func standingOf(standing git.Standing) agentv1.ReviewTreeStanding {
	switch standing {
	case git.StandingMoved:
		return agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_MOVED
	case git.StandingLanded:
		return agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_LANDED
	case git.StandingLandedMoved:
		return agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_LANDED_MOVED
	case git.StandingSame:
	}

	return agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_UNSPECIFIED
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
