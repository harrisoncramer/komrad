// Package assemble draws the agent and task rows the panel and the agent listing both show,
// once, from the resources a caller holds.
package assemble

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"komrad/lib/dates"
	"komrad/lib/xptr"

	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	taskv1 "komrad/proto/gen/task/v1"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"
)

// World is every resource a caller holds, by key.
type World struct {
	Agents        map[string]*agentv1.Agent
	Workflows     map[string]*workflowv1.Workflow
	Schedules     map[string]*schedulev1.Schedule
	Mail          map[string]*agentv1.Mail
	PullRequests  map[string]*pullrequestv1.PullRequest
	Repositories  map[string]*pullrequestv1.Repository
	Reviews       map[string]*agentv1.WatchedReview
	ReviewThreads map[string]*agentv1.ReviewThread
	Worktrees     map[string]*worktreev1.WatchedWorktree
	DocumentTypes map[string]*agentv1.DocumentType
	Tasks         map[string]*taskv1.Task
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
	case *watchv1.Row_DocumentType:
		w.DocumentTypes[row.DocumentType.GetId()] = row.DocumentType

		return watchv1.Resource_RESOURCE_DOCUMENT_TYPE, row.DocumentType.GetId()
	case *watchv1.Row_Task:
		w.Tasks[row.Task.GetId()] = row.Task

		return watchv1.Resource_RESOURCE_TASK, row.Task.GetId()
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
	if w.DocumentTypes == nil {
		w.DocumentTypes = map[string]*agentv1.DocumentType{}
	}
	if w.Tasks == nil {
		w.Tasks = map[string]*taskv1.Task{}
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
	case watchv1.Resource_RESOURCE_DOCUMENT_TYPE:
		delete(w.DocumentTypes, key)
	case watchv1.Resource_RESOURCE_TASK:
		delete(w.Tasks, key)
	case watchv1.Resource_RESOURCE_UNSPECIFIED:
	}
}

// AgentRows draws every agent in the world with its unread mail and its task's pull requests, and
// the task's local review on a top-level agent, newest first.
func AgentRows(w World, now time.Time) []*agentv1.AgentRow {
	unread := getAgentsWithUnreadMail(w.Mail, now)
	draw := newTaskDrawer(w)

	rows := make([]*agentv1.AgentRow, 0, len(w.Agents))
	for _, agent := range w.Agents {
		task := draw.row(agent.GetTaskId())

		row := &agentv1.AgentRow{
			Agent:        agent,
			UnreadMail:   unread[agent.GetId()],
			PullRequest:  task.PullRequest,
			PullRequests: task.PullRequests,
		}
		if agent.ParentAgentId == nil {
			row.LocalReviewRounds = task.LocalReviewRounds
			row.ReviewerComments = task.ReviewerComments
			row.PersonComments = task.PersonComments
		}

		rows = append(rows, row)
	}

	slices.SortFunc(rows, func(a, b *agentv1.AgentRow) int {
		return strings.Compare(b.GetAgent().GetId(), a.GetAgent().GetId())
	})

	return rows
}

// TaskRow is a task with the pull requests on its branch and where the local review of its work
// stands.
type TaskRow struct {
	Task         *taskv1.Task
	PullRequest  *agentv1.PullRequestStanding
	PullRequests []*pullrequestv1.PullRequest
	// LocalReviewRounds is how many rounds of its work were reviewed.
	LocalReviewRounds int32
	// ReviewerComments counts the comments reviewer agents left on its work, nil while there are none.
	ReviewerComments *agentv1.CommentCounts
	// PersonComments counts the comments the person left on its work, nil while there are none.
	PersonComments *agentv1.CommentCounts
}

// TaskRows draws every task in the world with its pull requests and local review, by task id.
func TaskRows(w World) map[string]TaskRow {
	draw := newTaskDrawer(w)

	rows := make(map[string]TaskRow, len(w.Tasks))
	for id := range w.Tasks {
		rows[id] = draw.row(id)
	}

	return rows
}

// taskDrawer draws task rows from the world's pull requests, reviews and threads, indexed once.
type taskDrawer struct {
	world    World
	onBranch map[branch][]*pullrequestv1.PullRequest
	reviewed map[string][]*agentv1.WatchedReview
	threaded map[string][]*agentv1.ReviewThread
}

func newTaskDrawer(w World) taskDrawer {
	return taskDrawer{
		world:    w,
		onBranch: groupPullRequestsByBranch(w.PullRequests),
		reviewed: groupReviewsByTask(w.Reviews),
		threaded: groupThreadsByTask(w.ReviewThreads),
	}
}

// row draws the task by id, which the world may not hold.
func (d taskDrawer) row(id string) TaskRow {
	task := d.world.Tasks[id]
	pullRequests := d.onBranch[getBranchForTask(d.world, task)]
	comments := countComments(d.reviewed[id], d.threaded[id])

	return TaskRow{
		Task:              task,
		PullRequest:       calculatePullRequestStanding(task, d.world.Repositories[task.GetRepositoryUrl()], pullRequests),
		PullRequests:      pullRequests,
		LocalReviewRounds: countLocalReviewRounds(d.reviewed[id]),
		ReviewerComments:  comments.reviewer,
		PersonComments:    comments.person,
	}
}

// branch is where a pull request was opened from.
type branch struct {
	repository     string
	headRepository string
	name           string
}

// getBranchForTask is the branch a task's pull requests are on, and no branch at all when it is
// the base branch the task's workflow cuts worktrees from.
func getBranchForTask(w World, task *taskv1.Task) branch {
	base := w.Workflows[task.GetWorkflowId()].GetWorktree().GetBaseBranch()
	if base != "" && task.GetBranch() == base {
		return branch{}
	}

	return branch{
		repository:     task.GetRepositoryUrl(),
		headRepository: task.GetRepositoryUrl(),
		name:           task.GetBranch(),
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

// calculatePullRequestStanding is where a task's pull request stands: unread until the sync
// has looked at the repository, none when it has and found nothing on the branch or the task's
// checkout has moved past a finished one, else the current one.
func calculatePullRequestStanding(task *taskv1.Task, repository *pullrequestv1.Repository, pullRequests []*pullrequestv1.PullRequest) *agentv1.PullRequestStanding {
	if len(pullRequests) == 0 {
		state := pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD
		if repository != nil {
			state = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE
		}

		return &agentv1.PullRequestStanding{State: state}
	}

	current := pullRequests[0]
	if isMovedPast(task, current) {
		return &agentv1.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE}
	}

	return &agentv1.PullRequestStanding{
		State:          current.GetState(),
		Number:         xptr.Ptr(current.GetNumber()),
		Url:            xptr.Ptr(current.GetUrl()),
		Title:          xptr.Ptr(current.GetTitle()),
		MergeReadiness: current.GetMergeReadiness(),
		HeadSha:        xptr.NonEmpty(current.GetHeadSha()),
		Forge:          xptr.NonEmpty(current.GetForge()),
	}
}

// isMovedPast is whether the task's checkout has left the commit a finished pull request ended on.
func isMovedPast(task *taskv1.Task, pullRequest *pullrequestv1.PullRequest) bool {
	if finishedRank(pullRequest) == 0 || task.HeadSha == nil {
		return false
	}

	return task.GetHeadSha() != pullRequest.GetHeadSha()
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
