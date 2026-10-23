package panel

import (
	"komrad/internal/assemble"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"
)

func agentFrom(s *agentv1.Agent) Agent {
	return Agent{
		ID:                s.GetId(),
		Name:              s.GetName(),
		Pane:              s.GetPaneId(),
		Cwd:               s.GetCwd(),
		Flags:             s.GetFlags(),
		Prompt:            s.GetPrompt(),
		State:             stateFrom(s.GetState()),
		Role:              roleFrom(s.GetRole()),
		FailureReason:     s.GetFailureReason(),
		TurnError:         s.GetTurnError(),
		Harness:           s.GetHarness(),
		SessionID:         s.GetSessionId(),
		WorkflowID:        s.GetWorkflowId(),
		ScheduleID:        s.ScheduleId,
		CreatedAt:         dates.Read(s.GetCreatedAt()),
		StateChangedAt:    dates.Read(s.GetStateChangedAt()),
		ArchivedAt:        dates.ReadPtr(s.GetArchivedAt()),
		TaskID:            s.GetTaskId(),
		ProvisionDisabled: s.GetProvisionDisabled(),
		Tool:              s.GetReportedReason(),
		Hooks:             s.GetHooks(),
	}
}

// tasksFrom is the assembled task rows as the panel keeps them, by task id.
func tasksFrom(rows map[string]assemble.TaskRow) Tasks {
	tasks := make(Tasks, len(rows))
	for id, row := range rows {
		tasks[id] = taskFrom(row)
	}

	return tasks
}

// taskFrom is one assembled task row as the panel keeps it.
func taskFrom(row assemble.TaskRow) Task {
	task := row.Task

	return Task{
		ID:                    task.GetId(),
		WorkflowID:            task.GetWorkflowId(),
		Slug:                  task.GetSlug(),
		Title:                 task.GetTitle(),
		Goal:                  task.GetGoal(),
		Notes:                 task.GetNotes(),
		NotesUpdatedAt:        dates.ReadPtr(task.GetNotesUpdatedAt()),
		CreatedAt:             dates.Read(task.GetCreatedAt()),
		ArchivedAt:            dates.ReadPtr(task.GetArchivedAt()),
		FailedRun:             task.GetFailedRun(),
		Documents:             task.GetDocuments(),
		Hidden:                task.GetHidden(),
		Parent:                task.GetParent(),
		Cwd:                   task.GetCwd(),
		Branch:                task.Branch,
		DiffStat:              task.GetDiffStat(),
		WorktreeRemovalReason: worktreeRemovalReasonFrom(task.GetWorktreeRemovalReason()),
		Sequence:              SequenceFrom(task.GetSequence()),
		MovedSinceSnapshot:    task.GetMovedSinceSnapshot(),
		Snapshots:             task.GetSnapshots(),
		PullRequest:           pullRequestStandingFrom(row.PullRequest),
		PullRequests:          xslice.Map(row.PullRequests, pullRequestFrom),
		ReviewerComments:      row.ReviewerComments,
		PersonComments:        row.PersonComments,
	}
}

// stateFrom is a wire state as the panel keeps it.
func stateFrom(state agentv1.AgentState) State {
	switch state {
	case agentv1.AgentState_AGENT_STATE_IDLE:
		return StateIdle
	case agentv1.AgentState_AGENT_STATE_THINKING:
		return StateThinking
	case agentv1.AgentState_AGENT_STATE_WAITING:
		return StateWaiting
	case agentv1.AgentState_AGENT_STATE_SETTING_UP:
		return StateSettingUp
	case agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE:
		return StateFailedToCreate
	case agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP:
		return StateFailedToCleanup
	case agentv1.AgentState_AGENT_STATE_ERRORED:
		return StateErrored
	case agentv1.AgentState_AGENT_STATE_UNCERTAIN, agentv1.AgentState_AGENT_STATE_UNSPECIFIED:
	}

	return StateUncertain
}

// roleFrom is a wire role as the panel keeps it.
func roleFrom(wire agentv1.AgentRole) role {
	switch wire {
	case agentv1.AgentRole_AGENT_ROLE_WRITER:
		return RoleWriter
	case agentv1.AgentRole_AGENT_ROLE_REVIEWER:
		return RoleReviewer
	case agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR:
		return RoleOrchestrator
	case agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED:
	}

	return ""
}

// worktreeRemovalReasonFrom is a wire removal reason as the panel keeps it.
func worktreeRemovalReasonFrom(reason taskv1.WorktreeRemovalReason) WorktreeRemovalReason {
	switch reason {
	case taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED:
		return WorktreeDeleted
	case taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE:
		return WorktreeGone
	case taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED:
	}

	return worktreeOnDisk
}

// SequenceFrom is a wire sequence as the panel keeps it.
func SequenceFrom(sequence taskv1.Sequence) Sequence {
	switch sequence {
	case taskv1.Sequence_SEQUENCE_REBASE:
		return SequenceRebase
	case taskv1.Sequence_SEQUENCE_AM:
		return sequenceApply
	case taskv1.Sequence_SEQUENCE_MERGE:
		return sequenceMerge
	case taskv1.Sequence_SEQUENCE_CHERRY_PICK:
		return sequenceCherryPick
	case taskv1.Sequence_SEQUENCE_REVERT:
		return sequenceRevert
	case taskv1.Sequence_SEQUENCE_UNSPECIFIED, taskv1.Sequence_SEQUENCE_NONE:
		return sequenceNone
	}

	return sequenceUnknown
}

// agentRowsFrom is a listing of assembled rows as the panel keeps them.
func agentRowsFrom(listed []*agentv1.AgentRow) []AgentRow {
	return xslice.Map(listed, agentRowFrom)
}

func agentRowFrom(row *agentv1.AgentRow) AgentRow {
	agent := row.GetAgent()

	return AgentRow{
		Agent:              agentFrom(agent),
		Alive:              agent.GetAlive(),
		UnreadMail:         row.GetUnreadMail(),
		FailedRun:          agent.GetFailedRun(),
		FailedRunLog:       agent.FailedRunLog,
		Documents:          agent.GetDocuments(),
		DocumentsUpdatedAt: dates.ReadPtr(agent.GetDocumentsUpdatedAt()),
	}
}

// pullRequestStandingFrom is the standing as the wire carries it.
func pullRequestStandingFrom(standing *agentv1.PullRequestStanding) PullRequestStanding {
	return PullRequestStanding{
		State:          standing.GetState(),
		Number:         standing.GetNumber(),
		URL:            standing.GetUrl(),
		Title:          standing.GetTitle(),
		HeadSHA:        standing.GetHeadSha(),
		Forge:          standing.GetForge(),
		MergeReadiness: mergeReadinessFrom(standing.GetMergeReadiness()),
	}
}

func pullRequestFrom(pullRequest *pullrequestv1.PullRequest) PullRequest {
	return PullRequest{
		ID:             pullRequest.GetId(),
		Forge:          pullRequest.GetForge(),
		Number:         pullRequest.GetNumber(),
		URL:            pullRequest.GetUrl(),
		Title:          pullRequest.GetTitle(),
		Author:         pullRequest.GetAuthor(),
		Body:           pullRequest.GetBody(),
		State:          pullRequest.GetState(),
		MergeReadiness: mergeReadinessFrom(pullRequest.GetMergeReadiness()),
		OpenedAt:       dates.Read(pullRequest.GetOpenedAt()),
		MergedAt:       dates.ReadPtr(pullRequest.GetMergedAt()),
		ClosedAt:       dates.ReadPtr(pullRequest.GetClosedAt()),
	}
}

func mergeReadinessFrom(mergeReadiness *pullrequestv1.MergeReadiness) MergeReadiness {
	return MergeReadiness{
		Decision:                mergeReadiness.GetDecision(),
		Checks:                  mergeReadiness.GetChecks(),
		Mergeability:            mergeReadiness.GetMergeability(),
		MergeabilityUnconfirmed: mergeReadiness.GetMergeabilityUnconfirmed(),
		AutoMerge:               mergeReadiness.GetAutoMerge(),
		AutoMergeable:           mergeReadiness.GetAutoMergeable(),
	}
}

func workflowsFrom(listed []*workflowv1.Workflow) []Workflow {
	return xslice.Map(listed, workflowFrom)
}

func workflowFrom(w *workflowv1.Workflow) Workflow {
	return Workflow{
		ID:              w.GetId(),
		Name:            w.GetName(),
		Context:         w.GetContext(),
		Harness:         w.GetHarness(),
		Cwd:             w.GetCwd(),
		Flags:           w.GetFlags(),
		ReviewerHarness: w.GetReviewerHarness(),
		ReviewerFlags:   w.GetReviewerFlags(),
		Worktree:        worktreeConfigFrom(w.GetWorktree()),
		Hooks: xslice.Map(w.GetHooks(), func(hook *workflowv1.WorkflowHook) Hook {
			return Hook{Moment: hook.GetEvent(), Script: hook.GetScript()}
		}),
		EventPrompts: xslice.Map(w.GetEventPrompts(), func(prompt *workflowv1.WorkflowEventPrompt) EventPrompt {
			return EventPrompt{
				Event:  prompt.GetEvent(),
				Prompt: prompt.GetPrompt(),
				Muted:  prompt.GetMuted(),
			}
		}),
		ArchivedAt: dates.ReadPtr(w.GetArchivedAt()),
	}
}

func worktreeConfigFrom(w *workflowv1.WorktreeConfig) WorktreeConfig {
	return WorktreeConfig{
		Enabled:      w.GetEnabled(),
		BaseBranch:   w.GetBaseBranch(),
		Dir:          w.GetDir(),
		SetupCommand: w.GetSetupCommand(),
		CopyPaths:    w.GetCopyPaths(),
		SymlinkPaths: w.GetSymlinkPaths(),
		DiffExclude:  w.GetDiffExclude(),
	}
}

func scheduleFrom(s *schedulev1.Schedule) Schedule {
	schedule := Schedule{
		ID:             s.GetId(),
		Name:           s.GetName(),
		Cron:           s.GetCron(),
		Timezone:       s.GetTimezone(),
		Enabled:        s.GetEnabled(),
		HideAgents:     s.GetHideAgents(),
		Prompt:         s.GetPrompt(),
		WorkflowID:     s.GetWorkflowId(),
		Kind:           s.GetKind(),
		OrchestratorID: s.GetOrchestratorAgentId(),
		RecentRuns:     xslice.Map(s.GetRecentRuns(), scheduleRunFrom),
	}

	if next := s.GetNextRunAt(); next != nil {
		schedule.NextRun = next.AsTime().Local()
	}

	if latest := s.GetLatestRun(); latest != nil {
		run := scheduleRunFrom(latest)
		schedule.LatestRun = &run
	}

	if archived := s.GetArchivedAt(); archived != nil {
		at := archived.AsTime().Local()
		schedule.ArchivedAt = &at
	}

	return schedule
}

func scheduleRunFrom(r *schedulev1.ScheduleRun) ScheduleRun {
	run := ScheduleRun{
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

// WorktreeFrom is one directory as a worktree read reports it.
func WorktreeFrom(w *worktreev1.Worktree) Worktree {
	item := Worktree{
		Dir:          w.GetDir(),
		RepoDir:      w.GetRepoDir(),
		Branch:       w.GetBranch(),
		Readable:     w.GetReadable(),
		Occupants:    xslice.Map(w.GetOccupants(), worktreeAgentFrom),
		LastActiveAt: dates.Read(w.GetLastActiveAt()),
		Fate:         worktreeFateFrom(w.GetFate()),
	}
	if task := w.GetTask(); task != nil {
		item.Task = &Task{
			ID:         task.GetId(),
			Slug:       task.GetSlug(),
			ArchivedAt: dates.ReadPtr(task.GetArchivedAt()),
		}
	}
	if w.MeasuredAt != nil {
		item.Size = &WorktreeSize{
			Bytes:      w.GetSizeBytes(),
			Share:      w.GetShareBytes(),
			MeasuredAt: dates.Read(w.GetMeasuredAt()),
			Err:        w.GetSizeError(),
		}
	}

	return item
}

func worktreeFateFrom(fate worktreev1.WorktreeFate) WorktreeFate {
	switch fate {
	case worktreev1.WorktreeFate_WORKTREE_FATE_ARCHIVED:
		return FateArchived
	case worktreev1.WorktreeFate_WORKTREE_FATE_LIVE:
		return FateLive
	case worktreev1.WorktreeFate_WORKTREE_FATE_DELETING:
		return FateDeleting
	case worktreev1.WorktreeFate_WORKTREE_FATE_UNOWNED, worktreev1.WorktreeFate_WORKTREE_FATE_UNSPECIFIED:
		return FateUnowned
	}

	return FateUnowned
}

func worktreeAgentFrom(a *worktreev1.WorktreeAgent) WorktreeAgent {
	return WorktreeAgent{
		ID:         a.GetId(),
		Name:       a.GetName(),
		State:      stateFrom(a.GetState()),
		ArchivedAt: dates.ReadPtr(a.GetArchivedAt()),
	}
}
