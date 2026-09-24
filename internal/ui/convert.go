package ui

import (
	"time"

	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	worktreev1 "weave/proto/gen/worktree/v1"

	"weave/internal/git"
	"weave/lib/dates"
	"weave/lib/xslice"
)

func agentFrom(s *agentv1.Agent) panel.Agent {
	metadata := xslice.Map(s.GetMetadata(), func(entry *agentv1.AgentMetadata) panel.Metadata {
		return panel.Metadata{Key: entry.GetKey(), Value: entry.GetValue()}
	})

	return panel.Agent{
		ID:                 s.GetId(),
		Name:               s.GetName(),
		Pane:               s.GetPaneId(),
		Cwd:                s.GetCwd(),
		Flags:              s.GetFlags(),
		Prompt:             s.GetPrompt(),
		State:              stateFrom(s.GetState()),
		Role:               roleFrom(s.GetRole()),
		ReportedReason:     s.GetReportedReason(),
		Harness:            s.GetHarness(),
		SessionID:          s.GetSessionId(),
		WorkflowID:         s.GetWorkflowId(),
		ScheduleID:         s.ScheduleId,
		CreatedAt:          dates.Parse(s.GetCreatedAt()),
		StateChangedAt:     dates.Parse(s.GetStateChangedAt()),
		ArchivedAt:         dates.ParsePtr(s.GetArchivedAt()),
		Hidden:             s.GetHidden(),
		Metadata:           metadata,
		Branch:             s.Branch,
		WorktreeRemoved:    s.GetWorktreeRemoved(),
		SequenceInProgress: s.GetSequenceInProgress(),
		ParentAgentID:      s.ParentAgentId,
		HeldAt:             dates.ParsePtr(s.GetHeldAt()),
	}
}

// stateFrom is a wire state as the panel keeps it.
func stateFrom(state agentv1.AgentState) panel.State {
	switch state {
	case agentv1.AgentState_AGENT_STATE_IDLE:
		return panel.StateIdle
	case agentv1.AgentState_AGENT_STATE_THINKING:
		return panel.StateThinking
	case agentv1.AgentState_AGENT_STATE_WAITING:
		return panel.StateWaiting
	case agentv1.AgentState_AGENT_STATE_SETTING_UP:
		return panel.StateSettingUp
	case agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE:
		return panel.StateFailedToCreate
	case agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP:
		return panel.StateFailedToCleanup
	case agentv1.AgentState_AGENT_STATE_UNCERTAIN, agentv1.AgentState_AGENT_STATE_UNSPECIFIED:
	}

	return panel.StateUncertain
}

// roleFrom is a wire role as the panel keeps it.
func roleFrom(role agentv1.AgentRole) panel.Role {
	switch role {
	case agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER:
		return panel.RoleImplementer
	case agentv1.AgentRole_AGENT_ROLE_REVIEWER:
		return panel.RoleReviewer
	case agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR:
		return panel.RoleOrchestrator
	case agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED:
	}

	return ""
}

// agentRowsFrom is a listing of assembled rows as the panel keeps them.
func agentRowsFrom(listed []*agentv1.AgentRow) []panel.AgentRow {
	return xslice.Map(listed, agentRowFrom)
}

func agentRowFrom(row *agentv1.AgentRow) panel.AgentRow {
	agent := row.GetAgent()

	return panel.AgentRow{
		Agent:           agentFrom(agent),
		Alive:           agent.GetAlive(),
		UnreadMail:      row.GetUnreadMail(),
		FailedRun:       agent.GetFailedRun(),
		FailedRunLog:    agent.FailedRunLog,
		LocalVerdict:    agent.GetLocalReviewVerdict(),
		LocalTree:       standingFrom(agent.GetLocalReviewTree()),
		PullRequest:     pullRequestStandingFrom(row.GetPullRequest()),
		PullRequests:    xslice.Map(row.GetPullRequests(), pullRequestFrom),
		MovedSinceRound: agent.GetMovedSinceRound(),
	}
}

// pullRequestStandingFrom is the standing as the wire carries it.
func pullRequestStandingFrom(standing *agentv1.PullRequestStanding) panel.PullRequestStanding {
	return panel.PullRequestStanding{
		State:        standing.GetState(),
		Checks:       standing.GetChecks(),
		Mergeability: standing.GetMergeability(),
		Number:       standing.GetNumber(),
		URL:          standing.GetUrl(),
		Title:        standing.GetTitle(),
	}
}

func pullRequestFrom(pullRequest *pullrequestv1.PullRequest) panel.PullRequest {
	return panel.PullRequest{
		Number:       pullRequest.GetNumber(),
		URL:          pullRequest.GetUrl(),
		Title:        pullRequest.GetTitle(),
		State:        pullRequest.GetState(),
		Checks:       pullRequest.GetChecks(),
		Mergeability: pullRequest.GetMergeability(),
		OpenedAt:     dates.Parse(pullRequest.GetOpenedAt()),
		MergedAt:     dates.ParsePtr(pullRequest.GetMergedAt()),
		ClosedAt:     dates.ParsePtr(pullRequest.GetClosedAt()),
	}
}

// standingFrom is a standing as the wire carries it.
func standingFrom(standing agentv1.ReviewTreeStanding) git.Standing {
	switch standing {
	case agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_MOVED:
		return git.StandingMoved
	case agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_LANDED:
		return git.StandingLanded
	case agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_LANDED_MOVED:
		return git.StandingLandedMoved
	case agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_UNSPECIFIED:
	}

	return git.StandingSame
}

func workflowsFrom(listed []*workflowv1.Workflow) []panel.Workflow {
	return xslice.Map(listed, workflowFrom)
}

func workflowFrom(w *workflowv1.Workflow) panel.Workflow {
	return panel.Workflow{
		ID:       w.GetId(),
		Name:     w.GetName(),
		Context:  w.GetContext(),
		Harness:  w.GetHarness(),
		Cwd:      w.GetCwd(),
		Flags:    w.GetFlags(),
		Worktree: worktreeConfigFrom(w.GetWorktree()),
		Hooks: xslice.Map(w.GetHooks(), func(hook *workflowv1.WorkflowHook) panel.Hook {
			return panel.Hook{Event: hook.GetEvent(), Script: hook.GetScript()}
		}),
		ArchivedAt: dates.ParsePtr(w.GetArchivedAt()),
	}
}

func worktreeConfigFrom(w *workflowv1.WorktreeConfig) panel.WorktreeConfig {
	return panel.WorktreeConfig{
		Enabled:      w.GetEnabled(),
		BaseBranch:   w.GetBaseBranch(),
		Dir:          w.GetDir(),
		CopyPaths:    w.GetCopyPaths(),
		SymlinkPaths: w.GetSymlinkPaths(),
		DiffExclude:  w.GetDiffExclude(),
	}
}

func scheduleItemsFrom(listed []*schedulev1.Schedule) []scheduleItem {
	return xslice.Map(listed, func(s *schedulev1.Schedule) scheduleItem {
		item := scheduleItem{schedule: scheduleFrom(s)}
		if next := s.GetNextRunAt(); next != nil {
			item.nextRun = next.AsTime().Local()
		}

		return item
	})
}

func scheduleFrom(s *schedulev1.Schedule) panel.Schedule {
	schedule := panel.Schedule{
		ID:         s.GetId(),
		Name:       s.GetName(),
		Cron:       s.GetCron(),
		Timezone:   s.GetTimezone(),
		Enabled:    s.GetEnabled(),
		HideAgents: s.GetHideAgents(),
		Prompt:     s.GetPrompt(),
		WorkflowID: s.GetWorkflowId(),
	}

	if latest := s.GetLatestRun(); latest != nil {
		run := scheduleRunFrom(latest)
		schedule.LatestRun = &run
	}

	return schedule
}

func scheduleRunFrom(r *schedulev1.ScheduleRun) panel.ScheduleRun {
	run := panel.ScheduleRun{
		ID:           r.GetId(),
		ScheduledFor: r.GetScheduledFor().AsTime().Local(),
		State:        r.GetState(),
		Attempt:      int(r.GetAttempt()),
		MaxAttempts:  int(r.GetMaxAttempts()),
		Message:      r.GetMessage(),
	}

	if at := r.GetFinalizedAt(); at != nil {
		finalizedAt := at.AsTime().Local()
		run.FinalizedAt = &finalizedAt
	}

	return run
}

func worktreeItemsFrom(listed *worktreev1.ListWorktreesResponse) []worktreeItem {
	return xslice.Map(listed.GetWorktrees(), worktreeItemFrom)
}

func worktreeItemFrom(w *worktreev1.Worktree) worktreeItem {
	item := worktreeItem{
		dir:          w.GetDir(),
		repoDir:      w.GetRepoDir(),
		branch:       w.GetBranch(),
		readable:     w.GetReadable(),
		occupants:    xslice.Map(w.GetOccupants(), worktreeAgentFrom),
		lastActiveAt: dates.Parse(w.GetLastActiveAt()),
		fate:         worktreeFateFrom(w.GetFate()),
	}
	if w.Agent != nil {
		agent := worktreeAgentFrom(w.GetAgent())
		item.agent = &agent
	}
	if w.MeasuredAt != nil {
		item.size = &worktreeSize{
			bytes:      w.GetSizeBytes(),
			measuredAt: dates.Parse(w.GetMeasuredAt()),
			err:        w.GetSizeError(),
		}
	}

	return item
}

func worktreeFateFrom(fate worktreev1.WorktreeFate) worktreeFate {
	switch fate {
	case worktreev1.WorktreeFate_WORKTREE_FATE_ARCHIVED:
		return fateArchived
	case worktreev1.WorktreeFate_WORKTREE_FATE_LIVE:
		return fateLive
	case worktreev1.WorktreeFate_WORKTREE_FATE_UNOWNED, worktreev1.WorktreeFate_WORKTREE_FATE_UNSPECIFIED:
		return fateUnowned
	}

	return fateUnowned
}

func worktreeAgentFrom(a *worktreev1.WorktreeAgent) worktreeAgent {
	return worktreeAgent{
		id:         a.GetId(),
		name:       a.GetName(),
		state:      stateFrom(a.GetState()),
		archivedAt: dates.ParsePtr(a.GetArchivedAt()),
	}
}

func worktreeInspectionFrom(read *worktreev1.GetWorktreeResponse, now time.Time) worktreeInspection {
	return worktreeInspection{
		answered: true,
		readAt:   now,
		item:     worktreeItemFrom(read.GetWorktree()),
		entries: xslice.Map(read.GetEntries(), func(entry *worktreev1.Entry) worktreeEntry {
			return worktreeEntry{
				path:  entry.GetPath(),
				bytes: entry.GetSizeBytes(),
				dir:   entry.GetDir(),
			}
		}),
		modified:    read.GetModified(),
		untracked:   read.GetUntracked(),
		baseRef:     read.GetBaseRef(),
		baseError:   read.GetBaseError(),
		ahead:       int(read.GetAhead()),
		behind:      int(read.GetBehind()),
		landed:      int(read.GetLandedCommits()),
		sequencing:  read.GetSequenceInProgress(),
		keepsBranch: read.GetKeepsBranch(),
		commits: xslice.Map(read.GetCommits(), func(commit *worktreev1.Commit) worktreeCommit {
			return worktreeCommit{
				sha:         commit.GetSha(),
				subject:     commit.GetSubject(),
				committedAt: dates.Parse(commit.GetCommittedAt()),
			}
		}),
	}
}
