package ui

import (
	"time"

	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	worktreev1 "weave/proto/gen/worktree/v1"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/dates"
	"weave/lib/xslice"
)

func protoToAgent(s *agentv1.Agent) agentstore.Agent {
	metadata := xslice.Map(s.GetMetadata(), func(entry *agentv1.AgentMetadata) agentstore.Metadata {
		return agentstore.Metadata{Key: entry.GetKey(), Value: entry.GetValue()}
	})

	return agentstore.Agent{
		ID:                 s.GetId(),
		Name:               s.GetName(),
		Pane:               s.GetPaneId(),
		Cwd:                s.GetCwd(),
		Flags:              s.GetFlags(),
		Prompt:             s.GetPrompt(),
		State:              agentstore.State(s.GetState()),
		Role:               agentstore.Role(s.GetRole()),
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

func agentItemsFrom(listed []*agentv1.Agent) []agentItem {
	return xslice.Map(listed, func(agent *agentv1.Agent) agentItem {
		return agentItem{
			agent:        protoToAgent(agent),
			alive:        agent.GetAlive(),
			unreadMail:   agent.GetUnreadMail(),
			failedRun:    agent.GetFailedRun(),
			failedRunLog: agent.FailedRunLog,
			localRounds:  agent.GetLocalReviewRounds(),
			localVerdict: agent.GetLocalReviewVerdict(),
			localTree:    standingFrom(agent.GetLocalReviewTree()),
			pullRequest:  pullRequestStandingFrom(agent.GetPullRequest()),
		}
	})
}

// pullRequestStandingFrom is the standing as the wire carries it.
func pullRequestStandingFrom(standing *agentv1.PullRequestStanding) pullRequestStanding {
	return pullRequestStanding{
		state:  standing.GetState(),
		number: standing.GetNumber(),
		url:    standing.GetUrl(),
		title:  standing.GetTitle(),
	}
}

func agentPullRequestsFrom(listed *pullrequestv1.ListAgentPullRequestsResponse) agentPullRequests {
	read := agentPullRequests{
		answered: true,
		readAt:   dates.ParsePtr(listed.GetRepositoryReadAt()),
		items:    make([]agentPullRequest, 0, len(listed.GetPullRequests())),
	}
	for _, pullRequest := range listed.GetPullRequests() {
		read.items = append(read.items, agentPullRequest{
			number:   pullRequest.GetNumber(),
			url:      pullRequest.GetUrl(),
			title:    pullRequest.GetTitle(),
			state:    pullRequest.GetState(),
			current:  pullRequest.GetCurrent(),
			openedAt: dates.Parse(pullRequest.GetOpenedAt()),
			mergedAt: dates.ParsePtr(pullRequest.GetMergedAt()),
			closedAt: dates.ParsePtr(pullRequest.GetClosedAt()),
		})
	}

	return read
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

func workflowsFrom(listed []*workflowv1.Workflow) []workflowstore.Workflow {
	return xslice.Map(listed, workflowFrom)
}

func workflowFrom(w *workflowv1.Workflow) workflowstore.Workflow {
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
		ArchivedAt: dates.ParsePtr(w.GetArchivedAt()),
	}
}

func archivedAgentsFrom(listed []*agentv1.Agent) []agentstore.Agent {
	return xslice.Map(listed, protoToAgent)
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

// marksFromAgents is the letter each agent answers to, keyed by the row it is drawn as.
func marksFromAgents(listed []*agentv1.Agent) map[string]string {
	marks := make(map[string]string, len(listed))
	for _, agent := range listed {
		if agent.Mark != nil {
			marks[agentRowKey(agent.GetId())] = agent.GetMark()
		}
	}

	return marks
}

// marksFromWorkflows is the same for the workflow headers, which answer to the same letters.
func marksFromWorkflows(listed []*workflowv1.Workflow) map[string]string {
	marks := make(map[string]string, len(listed))
	for _, workflow := range listed {
		if workflow.Mark != nil {
			marks[workflowRowKey(workflow.GetId())] = workflow.GetMark()
		}
	}

	return marks
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
		state:      agentstore.State(a.GetState()),
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
