package assemble_test

import (
	"testing"
	"time"

	"komrad/internal/assemble"
	"komrad/lib/dates"
	"komrad/lib/xptr"

	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	repo   = "https://github.com/owner/repo"
	fork   = "https://github.com/other/repo"
	older  = "01a0c000-0000-7000-8000-000000000001"
	newer  = "01a0c000-0000-7000-8000-000000000002"
	flowID = "01a0b000-0000-7000-8000-000000000001"
)

var now = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

func stamp(offset time.Duration) string {
	return dates.Stamp(now.Add(offset))
}

func agent(id string, set ...func(*agentv1.Agent)) *agentv1.Agent {
	a := &agentv1.Agent{
		Id:            id,
		WorkflowId:    flowID,
		Branch:        xptr.Ptr("agent/" + id),
		RepositoryUrl: xptr.Ptr(repo),
	}
	for _, f := range set {
		f(a)
	}

	return a
}

func readRepositories(urls ...string) map[string]*pullrequestv1.Repository {
	rows := map[string]*pullrequestv1.Repository{}
	for _, url := range urls {
		rows[url] = &pullrequestv1.Repository{Url: url, Read: true}
	}

	return rows
}

func at(sha string) func(*agentv1.Agent) {
	return func(a *agentv1.Agent) { a.HeadSha = xptr.Ptr(sha) }
}

func workflow(base string) *workflowv1.Workflow {
	return &workflowv1.Workflow{Id: flowID, Worktree: &workflowv1.WorktreeConfig{BaseBranch: base}}
}

func mail(id, to string, expiresIn time.Duration) *agentv1.Mail {
	return &agentv1.Mail{
		Id:        id,
		ToAgentId: to,
		ExpiresAt: stamp(expiresIn),
	}
}

type pullRequest struct {
	id, headRepo, branch string
	number               int32
	openedAgo            time.Duration
	merged               bool
	closed               bool
}

func (p pullRequest) row() *pullrequestv1.PullRequest {
	row := &pullrequestv1.PullRequest{
		Id:                p.id,
		RepositoryUrl:     repo,
		HeadRepositoryUrl: p.headRepo,
		HeadBranch:        p.branch,
		Number:            p.number,
		Url:               "https://github.com/owner/repo/pull/" + p.id,
		Title:             "pr " + p.id,
		HeadSha:           "sha-" + p.id,
		OpenedAt:          stamp(-p.openedAgo),
		State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		Checks:            pullrequestv1.Checks_CHECKS_PASSING,
		Mergeability:      pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
	}
	if p.merged {
		row.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
		row.MergedAt = xptr.Ptr(stamp(-p.openedAgo + time.Hour))
	}
	if p.closed {
		row.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED
		row.ClosedAt = xptr.Ptr(stamp(-p.openedAgo + time.Hour))
	}

	return row
}

func pullRequests(listed ...pullRequest) map[string]*pullrequestv1.PullRequest {
	rows := map[string]*pullrequestv1.PullRequest{}
	for _, p := range listed {
		rows[p.id] = p.row()
	}

	return rows
}

func standing(state pullrequestv1.PullRequestState) *agentv1.PullRequestStanding {
	return &agentv1.PullRequestStanding{State: state}
}

func currentStanding(p pullRequest) *agentv1.PullRequestStanding {
	row := p.row()

	return &agentv1.PullRequestStanding{
		State:        row.GetState(),
		Number:       xptr.Ptr(row.GetNumber()),
		Url:          xptr.Ptr(row.GetUrl()),
		Title:        xptr.Ptr(row.GetTitle()),
		Checks:       row.GetChecks(),
		Mergeability: row.GetMergeability(),
	}
}

// TestAgentRows covers every rule the row is drawn by: the mark whose agent it is, mail that
// is unclaimed and not yet expired, the pull requests on its branch unless that is the base
// branch, the current one first, and the standing when there is none.
func TestAgentRows(t *testing.T) {
	t.Parallel()

	onBranch := pullRequest{
		id:        "p1",
		headRepo:  repo,
		branch:    "agent/" + newer,
		number:    1,
		openedAgo: time.Hour,
	}
	oldest := pullRequest{
		id:        "p1",
		headRepo:  repo,
		branch:    "agent/" + newer,
		number:    1,
		openedAgo: 3 * time.Hour,
		merged:    true,
	}
	open := pullRequest{
		id:        "p2",
		headRepo:  repo,
		branch:    "agent/" + newer,
		number:    2,
		openedAgo: 2 * time.Hour,
	}
	latest := pullRequest{
		id:        "p3",
		headRepo:  repo,
		branch:    "agent/" + newer,
		number:    3,
		openedAgo: time.Hour,
		merged:    true,
	}

	closed := pullRequest{
		id:        "p4",
		headRepo:  repo,
		branch:    "agent/" + newer,
		number:    4,
		openedAgo: time.Hour,
		closed:    true,
	}

	tests := []struct {
		name  string
		world assemble.World
		want  []*agentv1.AgentRow
	}{
		{
			name:  "an agent nobody has read the repository of is unread",
			world: assemble.World{Agents: map[string]*agentv1.Agent{newer: agent(newer)}},
			want: []*agentv1.AgentRow{
				{Agent: agent(newer), PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD)},
			},
		},
		{
			name:  "a read repository with nothing on the branch is none",
			world: assemble.World{Agents: map[string]*agentv1.Agent{newer: agent(newer)}, Repositories: readRepositories(repo)},
			want: []*agentv1.AgentRow{
				{Agent: agent(newer), PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE)},
			},
		},
		{
			name:  "a read repository that is not the agent's leaves it unread",
			world: assemble.World{Agents: map[string]*agentv1.Agent{newer: agent(newer)}, Repositories: readRepositories(fork)},
			want: []*agentv1.AgentRow{
				{Agent: agent(newer), PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD)},
			},
		},
		{
			name: "the mark on the agent is drawn, and a workflow's is not",
			world: assemble.World{
				Agents: map[string]*agentv1.Agent{newer: agent(newer)},
				Marks: map[string]*agentv1.Mark{
					"mark-a": {
						Id:      "mark-a",
						Letter:  "a",
						AgentId: xptr.Ptr(newer),
					},
					"mark-w": {
						Id:         "mark-w",
						Letter:     "w",
						WorkflowId: xptr.Ptr(flowID),
					},
					"mark-o": {
						Id:      "mark-o",
						Letter:  "o",
						AgentId: xptr.Ptr(older),
					},
				},
			},
			want: []*agentv1.AgentRow{
				{
					Agent:       agent(newer),
					Mark:        xptr.Ptr("a"),
					PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD),
				},
			},
		},
		{
			name: "mail is unread while it has not expired, and only for the agent it is to",
			world: assemble.World{
				Agents: map[string]*agentv1.Agent{newer: agent(newer), older: agent(older)},
				Mail: map[string]*agentv1.Mail{
					"m1": mail("m1", newer, -time.Minute),
					"m2": mail("m2", older, time.Minute),
				},
			},
			want: []*agentv1.AgentRow{
				{Agent: agent(newer), PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD)},
				{
					Agent:       agent(older),
					UnreadMail:  true,
					PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD),
				},
			},
		},
		{
			name: "the pull request on the branch is the standing",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer)},
				PullRequests: pullRequests(onBranch),
				Repositories: readRepositories(repo),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer),
					PullRequest:  currentStanding(onBranch),
					PullRequests: []*pullrequestv1.PullRequest{onBranch.row()},
				},
			},
		},
		{
			name: "an unfinished pull request is current over a newer finished one, and the finished are newest first",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer)},
				PullRequests: pullRequests(oldest, open, latest),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer),
					PullRequest:  currentStanding(open),
					PullRequests: []*pullrequestv1.PullRequest{open.row(), latest.row(), oldest.row()},
				},
			},
		},
		{
			name: "a merged pull request stands while the checkout is still on its head",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, at("sha-p3"))},
				PullRequests: pullRequests(latest),
				Repositories: readRepositories(repo),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, at("sha-p3")),
					PullRequest:  currentStanding(latest),
					PullRequests: []*pullrequestv1.PullRequest{latest.row()},
				},
			},
		},
		{
			name: "a merged pull request is none once the checkout has moved off its head",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, at("elsewhere"))},
				PullRequests: pullRequests(oldest, latest),
				Repositories: readRepositories(repo),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, at("elsewhere")),
					PullRequest:  standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE),
					PullRequests: []*pullrequestv1.PullRequest{latest.row(), oldest.row()},
				},
			},
		},
		{
			name: "a closed pull request is none once the checkout has moved off its head",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, at("elsewhere"))},
				PullRequests: pullRequests(closed),
				Repositories: readRepositories(repo),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, at("elsewhere")),
					PullRequest:  standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE),
					PullRequests: []*pullrequestv1.PullRequest{closed.row()},
				},
			},
		},
		{
			name: "an open pull request stands whatever the checkout is on",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, at("elsewhere"))},
				PullRequests: pullRequests(latest, open),
				Repositories: readRepositories(repo),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, at("elsewhere")),
					PullRequest:  currentStanding(open),
					PullRequests: []*pullrequestv1.PullRequest{open.row(), latest.row()},
				},
			},
		},
		{
			name: "a merged pull request stands for a checkout whose head was never read",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer)},
				PullRequests: pullRequests(latest),
				Repositories: readRepositories(repo),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer),
					PullRequest:  currentStanding(latest),
					PullRequests: []*pullrequestv1.PullRequest{latest.row()},
				},
			},
		},
		{
			name: "a pull request from a fork, another branch or a branch nobody is on is not drawn",
			world: assemble.World{
				Agents: map[string]*agentv1.Agent{newer: agent(newer)},
				PullRequests: pullRequests(
					pullRequest{
						id:        "p1",
						headRepo:  fork,
						branch:    "agent/" + newer,
						number:    1,
						openedAgo: time.Hour,
					},
					pullRequest{
						id:        "p2",
						headRepo:  repo,
						branch:    "agent/" + older,
						number:    2,
						openedAgo: time.Hour,
					},
					pullRequest{
						id:        "p3",
						headRepo:  repo,
						branch:    "feature",
						number:    3,
						openedAgo: time.Hour,
					},
				),
				Repositories: readRepositories(repo),
			},
			want: []*agentv1.AgentRow{
				{Agent: agent(newer), PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE)},
			},
		},
		{
			name: "an agent on its workflow's base branch has no pull requests",
			world: assemble.World{
				Agents:    map[string]*agentv1.Agent{newer: agent(newer, func(a *agentv1.Agent) { a.Branch = xptr.Ptr("main") })},
				Workflows: map[string]*workflowv1.Workflow{flowID: workflow("main")},
				PullRequests: pullRequests(pullRequest{
					id:        "p1",
					headRepo:  repo,
					branch:    "main",
					number:    1,
					openedAgo: time.Hour,
				}),
				Repositories: readRepositories(repo),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:       agent(newer, func(a *agentv1.Agent) { a.Branch = xptr.Ptr("main") }),
					PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE),
				},
			},
		},
		{
			name: "an agent on another branch than the base keeps its pull requests",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer)},
				Workflows:    map[string]*workflowv1.Workflow{flowID: workflow("main")},
				PullRequests: pullRequests(onBranch),
				Repositories: readRepositories(repo),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer),
					PullRequest:  currentStanding(onBranch),
					PullRequests: []*pullrequestv1.PullRequest{onBranch.row()},
				},
			},
		},
		{
			name: "rows are newest first",
			world: assemble.World{
				Agents: map[string]*agentv1.Agent{older: agent(older), newer: agent(newer)},
			},
			want: []*agentv1.AgentRow{
				{Agent: agent(newer), PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD)},
				{Agent: agent(older), PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, assemble.AgentRows(tt.world, now))
		})
	}
}

// TestMarks is the letter by the id of the agent or workflow it is on, and a mark on neither
// is nowhere.
func TestMarks(t *testing.T) {
	t.Parallel()

	world := assemble.World{Marks: map[string]*agentv1.Mark{
		"mark-a": {
			Id:      "mark-a",
			Letter:  "a",
			AgentId: xptr.Ptr(newer),
		},
		"mark-w": {
			Id:         "mark-w",
			Letter:     "w",
			WorkflowId: xptr.Ptr(flowID),
		},
		"mark-x": {Id: "mark-x", Letter: "x"},
	}}

	assert.Equal(t, map[string]string{newer: "a", flowID: "w"}, assemble.Marks(world))
}

func put(row *watchv1.Row) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Puts: []*watchv1.Row{row}}}}
}

func deleted(resource watchv1.Resource, key string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Deleted: []*watchv1.Deleted{{Resource: resource, Key: key}}}}}
}

func agentRow(a *agentv1.Agent) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: a}}
}

// TestApplyOrdering is the promise that events need no order: a child that arrives before its
// parent is held and drawn once the parent arrives, and a parent's tombstone stops its children
// being drawn without dropping them.
func TestApplyOrdering(t *testing.T) {
	t.Parallel()

	var world assemble.World

	world.Apply(put(&watchv1.Row{Row: &watchv1.Row_Mark{Mark: &agentv1.Mark{
		Id:      "mark-a",
		Letter:  "a",
		AgentId: xptr.Ptr(newer),
	}}}))
	world.Apply(put(&watchv1.Row{Row: &watchv1.Row_Mail{Mail: mail("m1", newer, time.Minute)}}))
	assert.Empty(t, assemble.AgentRows(world, now), "a mark and a mail whose agent is not held draw nothing")

	world.Apply(put(agentRow(agent(newer))))
	rows := assemble.AgentRows(world, now)
	require.Len(t, rows, 1)
	assert.Equal(t, xptr.Ptr("a"), rows[0].Mark)
	assert.True(t, rows[0].UnreadMail)

	world.Apply(deleted(watchv1.Resource_RESOURCE_AGENT, newer))
	assert.Empty(t, assemble.AgentRows(world, now), "the tombstone drops the row and everything drawn on it")
	assert.Len(t, world.Marks, 1, "the children stay held for their own tombstones")
	assert.Len(t, world.Mail, 1)
}

// TestApplyRepository pins that an agent reads as unread until its repository is held as read.
func TestApplyRepository(t *testing.T) {
	t.Parallel()

	var world assemble.World

	world.Apply(put(agentRow(agent(newer))))
	world.Apply(put(&watchv1.Row{Row: &watchv1.Row_Repository{Repository: &pullrequestv1.Repository{Url: repo, Read: true}}}))
	assert.Equal(t, standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE), assemble.AgentRows(world, now)[0].PullRequest)

	world.Apply(deleted(watchv1.Resource_RESOURCE_REPOSITORY, repo))
	assert.Empty(t, world.Repositories)
	assert.Equal(t, standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD), assemble.AgentRows(world, now)[0].PullRequest)
}

// TestApplyReportsChanged is which keys a message reports touching, by resource.
func TestApplyReportsChanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  *watchv1.WatchEvent
		want assemble.Changed
	}{
		{
			name: "a nil message touches nothing",
			msg:  nil,
			want: assemble.Changed{},
		},
		{
			name: "a heartbeat touches nothing",
			msg:  &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Heartbeat{}},
			want: assemble.Changed{},
		},
		{
			name: "a row that carries nothing is not reported",
			msg:  put(&watchv1.Row{}),
			want: assemble.Changed{},
		},
		{
			name: "a put reports its key",
			msg:  put(agentRow(agent(newer))),
			want: assemble.Changed{Keys: map[watchv1.Resource][]string{watchv1.Resource_RESOURCE_AGENT: {newer}}},
		},
		{
			name: "a tombstone reports its key",
			msg:  deleted(watchv1.Resource_RESOURCE_MAIL, "m1"),
			want: assemble.Changed{Keys: map[watchv1.Resource][]string{watchv1.Resource_RESOURCE_MAIL: {"m1"}}},
		},
		{
			name: "a snapshot reports the world replaced and every row it carries",
			msg: &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{
				Rows: []*watchv1.Row{
					agentRow(agent(older)),
					{Row: &watchv1.Row_Mark{Mark: &agentv1.Mark{Id: "mark-a", Letter: "a"}}},
				},
			}}},
			want: assemble.Changed{
				Replaced: true,
				Keys: map[watchv1.Resource][]string{
					watchv1.Resource_RESOURCE_AGENT: {older},
					watchv1.Resource_RESOURCE_MARK:  {"mark-a"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var world assemble.World

			assert.Equal(t, tt.want, world.Apply(tt.msg))
		})
	}
}

// TestApplySnapshotReplacesTheWorld is the promise that a snapshot keeps no row it does not carry.
func TestApplySnapshotReplacesTheWorld(t *testing.T) {
	t.Parallel()

	var world assemble.World
	world.Apply(put(agentRow(agent(newer))))
	world.Apply(put(&watchv1.Row{Row: &watchv1.Row_Mark{Mark: &agentv1.Mark{Id: "mark-a", Letter: "a"}}}))

	world.Apply(&watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{
		Rows: []*watchv1.Row{agentRow(agent(older))},
	}}})

	assert.Equal(t, map[string]*agentv1.Agent{older: agent(older)}, world.Agents)
	assert.Empty(t, world.Marks)
}

// TestChangedMerge is how a batch's changes fold together, a snapshot discarding what came
// before it and keeping what came after.
func TestChangedMerge(t *testing.T) {
	t.Parallel()

	agents := func(keys ...string) assemble.Changed {
		return assemble.Changed{Keys: map[watchv1.Resource][]string{watchv1.Resource_RESOURCE_AGENT: keys}}
	}
	snapshot := agents(older)
	snapshot.Replaced = true

	tests := []struct {
		name   string
		merged []assemble.Changed
		want   assemble.Changed
	}{
		{
			name:   "nothing merged is nothing changed",
			merged: nil,
			want:   assemble.Changed{},
		},
		{
			name:   "a snapshot drops the changes before it",
			merged: []assemble.Changed{agents(newer), snapshot},
			want:   snapshot,
		},
		{
			name:   "changes after a snapshot are kept beside it",
			merged: []assemble.Changed{snapshot, agents(newer)},
			want: assemble.Changed{
				Replaced: true,
				Keys:     map[watchv1.Resource][]string{watchv1.Resource_RESOURCE_AGENT: {older, newer}},
			},
		},
		{
			name:   "changes gather by resource",
			merged: []assemble.Changed{agents(newer), agents(older)},
			want:   agents(newer, older),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var changed assemble.Changed
			for _, later := range tt.merged {
				changed.Merge(later)
			}

			assert.Equal(t, tt.want, changed)
		})
	}
}
