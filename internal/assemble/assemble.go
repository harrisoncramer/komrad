// Package assemble draws the agent row the panel and the agent listing both show, once, from
// the resources a caller holds.
package assemble

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"weave/lib/dates"
	"weave/lib/xptr"

	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
)

// World is every resource a caller holds, by key.
type World struct {
	Agents       map[string]*agentv1.Agent
	Workflows    map[string]*workflowv1.Workflow
	Schedules    map[string]*schedulev1.Schedule
	Marks        map[string]*agentv1.Mark
	Mail         map[string]*agentv1.Mail
	PullRequests map[string]*pullrequestv1.PullRequest
}

// Apply puts or drops the row one message names, and reports the agents whose key or branch it
// had not seen.
func (w *World) Apply(msg *watchv1.WatchEvent) (arrived []string) {
	switch msg := msg.GetEvent().(type) {
	case *watchv1.WatchEvent_Snapshot:
		*w = World{}
		for _, row := range msg.Snapshot.GetRows() {
			w.Put(row)
		}
	case *watchv1.WatchEvent_Put:
		return w.Put(msg.Put)
	case *watchv1.WatchEvent_Deleted:
		w.drop(msg.Deleted)
	case *watchv1.WatchEvent_Heartbeat, nil:
	}

	return nil
}

// Put holds one row, reporting the agent when it is one whose key or branch the world had not
// seen.
func (w *World) Put(row *watchv1.Row) (arrived []string) {
	w.ensureMaps()

	switch row := row.GetRow().(type) {
	case *watchv1.Row_Agent:
		held, ok := w.Agents[row.Agent.GetId()]
		w.Agents[row.Agent.GetId()] = row.Agent
		if !ok || held.GetRepositoryUrl() != row.Agent.GetRepositoryUrl() || held.GetBranch() != row.Agent.GetBranch() {
			return []string{row.Agent.GetId()}
		}
	case *watchv1.Row_Workflow:
		w.Workflows[row.Workflow.GetId()] = row.Workflow
	case *watchv1.Row_Schedule:
		w.Schedules[row.Schedule.GetId()] = row.Schedule
	case *watchv1.Row_Mark:
		w.Marks[row.Mark.GetId()] = row.Mark
	case *watchv1.Row_Mail:
		w.Mail[row.Mail.GetId()] = row.Mail
	case *watchv1.Row_PullRequest:
		w.PullRequests[row.PullRequest.GetId()] = row.PullRequest
	case nil:
	}

	return nil
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
	case watchv1.Resource_RESOURCE_UNSPECIFIED:
	}
}

// AgentRows draws every agent in the world with its mark, its unread mail and its pull
// requests, newest first.
func AgentRows(w World, now time.Time) []*agentv1.AgentRow {
	marks := Marks(w)
	unread := getAgentsWithUnreadMail(w.Mail, now)
	onBranch := groupPullRequestsByBranch(w.PullRequests)

	rows := make([]*agentv1.AgentRow, 0, len(w.Agents))
	for _, agent := range w.Agents {
		pullRequests := onBranch[getBranchForAgent(w, agent)]

		row := &agentv1.AgentRow{
			Agent:        agent,
			UnreadMail:   unread[agent.GetId()],
			PullRequest:  calculatePullRequestStanding(agent, pullRequests),
			PullRequests: pullRequests,
		}
		if mark, ok := marks[agent.GetId()]; ok {
			row.Mark = &mark
		}

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
// has looked at the repository, none when it has and found nothing on the branch, else the
// current one.
func calculatePullRequestStanding(agent *agentv1.Agent, pullRequests []*pullrequestv1.PullRequest) *agentv1.PullRequestStanding {
	if len(pullRequests) == 0 {
		state := pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD
		if agent.GetRepositoryRead() {
			state = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE
		}

		return &agentv1.PullRequestStanding{State: state}
	}

	current := pullRequests[0]

	return &agentv1.PullRequestStanding{
		State:        current.GetState(),
		Number:       xptr.Ptr(current.GetNumber()),
		Url:          xptr.Ptr(current.GetUrl()),
		Title:        xptr.Ptr(current.GetTitle()),
		Checks:       current.GetChecks(),
		Mergeability: current.GetMergeability(),
	}
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
