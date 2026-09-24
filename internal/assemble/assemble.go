// Package assemble draws the agent row the panel and the agent listing both show, once, from
// the resources a caller holds.
package assemble

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"komrade/lib/dates"
	"komrade/lib/xptr"

	agentv1 "komrade/proto/gen/agent/v1"
	pullrequestv1 "komrade/proto/gen/pullrequest/v1"
	schedulev1 "komrade/proto/gen/schedule/v1"
	watchv1 "komrade/proto/gen/watch/v1"
	workflowv1 "komrade/proto/gen/workflow/v1"
	worktreev1 "komrade/proto/gen/worktree/v1"
)

// World is every resource a caller holds, by key.
type World struct {
	Agents        map[string]*agentv1.Agent
	Workflows     map[string]*workflowv1.Workflow
	Schedules     map[string]*schedulev1.Schedule
	Marks         map[string]*agentv1.Mark
	Mail          map[string]*agentv1.Mail
	PullRequests  map[string]*pullrequestv1.PullRequest
	Repositories  map[string]*pullrequestv1.Repository
	Reviews       map[string]*agentv1.WatchedReview
	ReviewThreads map[string]*agentv1.ReviewThread
	Worktrees     map[string]*worktreev1.WatchedWorktree
}

// Changed is the keys a message put or dropped, by resource.
type Changed struct {
	// Replaced is whether a snapshot replaced the world whole.
	Replaced bool
	Keys     map[watchv1.Resource][]string
}

// Merge folds a later change into this one, a snapshot discarding what came before it.
func (c *Changed) Merge(later Changed) {
	if later.Replaced {
		*c = Changed{Replaced: true}
	}
	for resource, keys := range later.Keys {
		for _, key := range keys {
			c.add(resource, key)
		}
	}
}

func (c *Changed) add(resource watchv1.Resource, key string) {
	if resource == watchv1.Resource_RESOURCE_UNSPECIFIED {
		return
	}
	if c.Keys == nil {
		c.Keys = map[watchv1.Resource][]string{}
	}
	c.Keys[resource] = append(c.Keys[resource], key)
}

// Apply puts or drops the rows one message names, and reports the keys it touched.
func (w *World) Apply(msg *watchv1.WatchEvent) Changed {
	var changed Changed
	switch msg := msg.GetEvent().(type) {
	case *watchv1.WatchEvent_Snapshot:
		*w = World{}
		changed.Replaced = true
		for _, row := range msg.Snapshot.GetRows() {
			changed.add(w.put(row))
		}
	case *watchv1.WatchEvent_Changes:
		for _, row := range msg.Changes.GetPuts() {
			changed.add(w.put(row))
		}
		for _, deleted := range msg.Changes.GetDeleted() {
			w.drop(deleted)
			changed.add(deleted.GetResource(), deleted.GetKey())
		}
	case *watchv1.WatchEvent_Heartbeat, nil:
	}

	return changed
}

// Put holds one row.
func (w *World) Put(row *watchv1.Row) {
	w.put(row)
}

func (w *World) put(row *watchv1.Row) (watchv1.Resource, string) {
	w.ensureMaps()

	switch row := row.GetRow().(type) {
	case *watchv1.Row_Agent:
		w.Agents[row.Agent.GetId()] = row.Agent

		return watchv1.Resource_RESOURCE_AGENT, row.Agent.GetId()
	case *watchv1.Row_Workflow:
		w.Workflows[row.Workflow.GetId()] = row.Workflow

		return watchv1.Resource_RESOURCE_WORKFLOW, row.Workflow.GetId()
	case *watchv1.Row_Schedule:
		w.Schedules[row.Schedule.GetId()] = row.Schedule

		return watchv1.Resource_RESOURCE_SCHEDULE, row.Schedule.GetId()
	case *watchv1.Row_Mark:
		w.Marks[row.Mark.GetId()] = row.Mark

		return watchv1.Resource_RESOURCE_MARK, row.Mark.GetId()
	case *watchv1.Row_Mail:
		w.Mail[row.Mail.GetId()] = row.Mail

		return watchv1.Resource_RESOURCE_MAIL, row.Mail.GetId()
	case *watchv1.Row_PullRequest:
		w.PullRequests[row.PullRequest.GetId()] = row.PullRequest

		return watchv1.Resource_RESOURCE_PULL_REQUEST, row.PullRequest.GetId()
	case *watchv1.Row_Repository:
		w.Repositories[row.Repository.GetUrl()] = row.Repository

		return watchv1.Resource_RESOURCE_REPOSITORY, row.Repository.GetUrl()
	case *watchv1.Row_Review:
		w.Reviews[row.Review.GetId()] = row.Review

		return watchv1.Resource_RESOURCE_REVIEW, row.Review.GetId()
	case *watchv1.Row_ReviewThread:
		w.ReviewThreads[row.ReviewThread.GetId()] = row.ReviewThread

		return watchv1.Resource_RESOURCE_REVIEW_THREAD, row.ReviewThread.GetId()
	case *watchv1.Row_Worktree:
		w.Worktrees[row.Worktree.GetDir()] = row.Worktree

		return watchv1.Resource_RESOURCE_WORKTREE, row.Worktree.GetDir()
	case nil:
	}

	return watchv1.Resource_RESOURCE_UNSPECIFIED, ""
}

func (w *World) ensureMaps() {
	if w.Agents == nil {
		w.Agents = map[string]*agentv1.Agent{}
	}
	if w.Workflows == nil {
		w.Workflows = map[string]*workflowv1.Workflow{}
	}
	if w.Schedules == nil {
		w.Schedules = map[string]*schedulev1.Schedule{}
	}
	if w.Marks == nil {
		w.Marks = map[string]*agentv1.Mark{}
	}
	if w.Mail == nil {
		w.Mail = map[string]*agentv1.Mail{}
	}
	if w.PullRequests == nil {
		w.PullRequests = map[string]*pullrequestv1.PullRequest{}
	}
	if w.Repositories == nil {
		w.Repositories = map[string]*pullrequestv1.Repository{}
	}
	if w.Reviews == nil {
		w.Reviews = map[string]*agentv1.WatchedReview{}
	}
	if w.ReviewThreads == nil {
		w.ReviewThreads = map[string]*agentv1.ReviewThread{}
	}
	if w.Worktrees == nil {
		w.Worktrees = map[string]*worktreev1.WatchedWorktree{}
	}
}

func (w *World) drop(deleted *watchv1.Deleted) {
	key := deleted.GetKey()
	switch deleted.GetResource() {
	case watchv1.Resource_RESOURCE_AGENT:
		delete(w.Agents, key)
	case watchv1.Resource_RESOURCE_WORKFLOW:
		delete(w.Workflows, key)
	case watchv1.Resource_RESOURCE_SCHEDULE:
		delete(w.Schedules, key)
	case watchv1.Resource_RESOURCE_MARK:
		delete(w.Marks, key)
	case watchv1.Resource_RESOURCE_MAIL:
		delete(w.Mail, key)
	case watchv1.Resource_RESOURCE_PULL_REQUEST:
		delete(w.PullRequests, key)
	case watchv1.Resource_RESOURCE_REPOSITORY:
		delete(w.Repositories, key)
	case watchv1.Resource_RESOURCE_REVIEW:
		delete(w.Reviews, key)
	case watchv1.Resource_RESOURCE_REVIEW_THREAD:
		delete(w.ReviewThreads, key)
	case watchv1.Resource_RESOURCE_WORKTREE:
		delete(w.Worktrees, key)
	case watchv1.Resource_RESOURCE_UNSPECIFIED:
	}
}

// AgentRows draws every agent in the world with its mark, its unread mail, its pull requests
// and where the local review of its work stands, newest first.
func AgentRows(w World, now time.Time) []*agentv1.AgentRow {
	marks := Marks(w)
	unread := getAgentsWithUnreadMail(w.Mail, now)
	onBranch := groupPullRequestsByBranch(w.PullRequests)
	reviewed := groupReviewsByAgent(w.Reviews)

	rows := make([]*agentv1.AgentRow, 0, len(w.Agents))
	for _, agent := range w.Agents {
		pullRequests := onBranch[getBranchForAgent(w, agent)]

		row := &agentv1.AgentRow{
			Agent:        agent,
			UnreadMail:   unread[agent.GetId()],
			PullRequest:  calculatePullRequestStanding(agent, w.Repositories[agent.GetRepositoryUrl()], pullRequests),
			PullRequests: pullRequests,
		}
		if mark, ok := marks[agent.GetId()]; ok {
			row.Mark = &mark
		}
		setLocalReview(row, reviewed[agent.GetId()])

		rows = append(rows, row)
	}

	slices.SortFunc(rows, func(a, b *agentv1.AgentRow) int {
		return strings.Compare(b.GetAgent().GetId(), a.GetAgent().GetId())
	})

	return rows
}

// Marks is the letter on each row, agent or workflow, by the id of the one it is on.
func Marks(w World) map[string]string {
	marks := make(map[string]string, len(w.Marks))
	for _, mark := range w.Marks {
		switch {
		case mark.AgentId != nil:
			marks[mark.GetAgentId()] = mark.GetMark()
		case mark.WorkflowId != nil:
			marks[mark.GetWorkflowId()] = mark.GetMark()
		}
	}

	return marks
}

// branch is where a pull request was opened from.
type branch struct {
	repository     string
	headRepository string
	name           string
}

// getBranchForAgent is the branch an agent's pull requests are on, and no branch at all when
// it is the base branch its workflow cuts worktrees from.
func getBranchForAgent(w World, agent *agentv1.Agent) branch {
	base := w.Workflows[agent.GetWorkflowId()].GetWorktree().GetBaseBranch()
	if base != "" && agent.GetBranch() == base {
		return branch{}
	}

	return branch{
		repository:     agent.GetRepositoryUrl(),
		headRepository: agent.GetRepositoryUrl(),
		name:           agent.GetBranch(),
	}
}

// groupPullRequestsByBranch indexes the pull requests by the branch they were opened from, each
// branch's current one first.
func groupPullRequestsByBranch(pullRequests map[string]*pullrequestv1.PullRequest) map[branch][]*pullrequestv1.PullRequest {
	onBranch := map[branch][]*pullrequestv1.PullRequest{}
	for _, pullRequest := range pullRequests {
		key := branch{
			repository:     pullRequest.GetRepositoryUrl(),
			headRepository: pullRequest.GetHeadRepositoryUrl(),
			name:           pullRequest.GetHeadBranch(),
		}
		onBranch[key] = append(onBranch[key], pullRequest)
	}

	for _, listed := range onBranch {
		slices.SortFunc(listed, func(a, b *pullrequestv1.PullRequest) int {
			return cmp.Or(
				cmp.Compare(finishedRank(a), finishedRank(b)),
				dates.Parse(b.GetOpenedAt()).Compare(dates.Parse(a.GetOpenedAt())),
				strings.Compare(b.GetId(), a.GetId()),
			)
		})
	}

	return onBranch
}

// finishedRank is 1 for a pull request that has merged or closed and 0 for one still open.
func finishedRank(pullRequest *pullrequestv1.PullRequest) int {
	if pullRequest.MergedAt != nil || pullRequest.ClosedAt != nil {
		return 1
	}

	return 0
}

// calculatePullRequestStanding is where an agent's pull request stands: unread until the sync
// has looked at the repository, none when it has and found nothing on the branch or the agent
// has moved past a finished one, else the current one.
func calculatePullRequestStanding(agent *agentv1.Agent, repository *pullrequestv1.Repository, pullRequests []*pullrequestv1.PullRequest) *agentv1.PullRequestStanding {
	if len(pullRequests) == 0 {
		state := pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD
		if repository.GetRead() {
			state = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE
		}

		return &agentv1.PullRequestStanding{State: state}
	}

	current := pullRequests[0]
	if isMovedPast(agent, current) {
		return &agentv1.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE}
	}

	return &agentv1.PullRequestStanding{
		State:        current.GetState(),
		Number:       xptr.Ptr(current.GetNumber()),
		Url:          xptr.Ptr(current.GetUrl()),
		Title:        xptr.Ptr(current.GetTitle()),
		Checks:       current.GetChecks(),
		Mergeability: current.GetMergeability(),
	}
}

// isMovedPast is whether the agent's checkout has left the commit a finished pull request ended on.
func isMovedPast(agent *agentv1.Agent, pullRequest *pullrequestv1.PullRequest) bool {
	if finishedRank(pullRequest) == 0 || agent.HeadSha == nil {
		return false
	}

	return agent.GetHeadSha() != pullRequest.GetHeadSha()
}

// getAgentsWithUnreadMail is the set of agents with mail waiting that has not expired by now.
func getAgentsWithUnreadMail(mail map[string]*agentv1.Mail, now time.Time) map[string]bool {
	unread := map[string]bool{}
	for _, piece := range mail {
		if dates.Parse(piece.GetExpiresAt()).After(now) {
			unread[piece.GetToAgentId()] = true
		}
	}

	return unread
}
