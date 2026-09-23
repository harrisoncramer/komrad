package assemble_test

import (
	"testing"
	"time"

	"weave/internal/assemble"
	"weave/lib/dates"
	"weave/lib/xptr"

	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

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

func read(a *agentv1.Agent) { a.RepositoryRead = true }

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
			world: assemble.World{Agents: map[string]*agentv1.Agent{newer: agent(newer, read)}},
			want: []*agentv1.AgentRow{
				{Agent: agent(newer, read), PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE)},
			},
		},
		{
			name: "the mark on the agent is drawn, and a workflow's is not",
			world: assemble.World{
				Agents: map[string]*agentv1.Agent{newer: agent(newer)},
				Marks: map[string]*agentv1.Mark{
					"mark-a": {
						Id:      "mark-a",
						Mark:    "a",
						AgentId: xptr.Ptr(newer),
					},
					"mark-w": {
						Id:         "mark-w",
						Mark:       "w",
						WorkflowId: xptr.Ptr(flowID),
					},
					"mark-o": {
						Id:      "mark-o",
						Mark:    "o",
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
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, read)},
				PullRequests: pullRequests(onBranch),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, read),
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
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, read, at("sha-p3"))},
				PullRequests: pullRequests(latest),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, read, at("sha-p3")),
					PullRequest:  currentStanding(latest),
					PullRequests: []*pullrequestv1.PullRequest{latest.row()},
				},
			},
		},
		{
			name: "a merged pull request is none once the checkout has moved off its head",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, read, at("elsewhere"))},
				PullRequests: pullRequests(oldest, latest),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, read, at("elsewhere")),
					PullRequest:  standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE),
					PullRequests: []*pullrequestv1.PullRequest{latest.row(), oldest.row()},
				},
			},
		},
		{
			name: "a closed pull request is none once the checkout has moved off its head",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, read, at("elsewhere"))},
				PullRequests: pullRequests(closed),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, read, at("elsewhere")),
					PullRequest:  standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE),
					PullRequests: []*pullrequestv1.PullRequest{closed.row()},
				},
			},
		},
		{
			name: "an open pull request stands whatever the checkout is on",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, read, at("elsewhere"))},
				PullRequests: pullRequests(latest, open),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, read, at("elsewhere")),
					PullRequest:  currentStanding(open),
					PullRequests: []*pullrequestv1.PullRequest{open.row(), latest.row()},
				},
			},
		},
		{
			name: "a merged pull request stands for a checkout whose head was never read",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, read)},
				PullRequests: pullRequests(latest),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, read),
					PullRequest:  currentStanding(latest),
					PullRequests: []*pullrequestv1.PullRequest{latest.row()},
				},
			},
		},
		{
			name: "a pull request from a fork, another branch or a branch nobody is on is not drawn",
			world: assemble.World{
				Agents: map[string]*agentv1.Agent{newer: agent(newer, read)},
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
			},
			want: []*agentv1.AgentRow{
				{Agent: agent(newer, read), PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE)},
			},
		},
		{
			name: "an agent on its workflow's base branch has no pull requests",
			world: assemble.World{
				Agents:    map[string]*agentv1.Agent{newer: agent(newer, read, func(a *agentv1.Agent) { a.Branch = xptr.Ptr("main") })},
				Workflows: map[string]*workflowv1.Workflow{flowID: workflow("main")},
				PullRequests: pullRequests(pullRequest{
					id:        "p1",
					headRepo:  repo,
					branch:    "main",
					number:    1,
					openedAgo: time.Hour,
				}),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:       agent(newer, read, func(a *agentv1.Agent) { a.Branch = xptr.Ptr("main") }),
					PullRequest: standing(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE),
				},
			},
		},
		{
			name: "an agent on another branch than the base keeps its pull requests",
			world: assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer, read)},
				Workflows:    map[string]*workflowv1.Workflow{flowID: workflow("main")},
				PullRequests: pullRequests(onBranch),
			},
			want: []*agentv1.AgentRow{
				{
					Agent:        agent(newer, read),
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
			Mark:    "a",
			AgentId: xptr.Ptr(newer),
		},
		"mark-w": {
			Id:         "mark-w",
			Mark:       "w",
			WorkflowId: xptr.Ptr(flowID),
		},
		"mark-x": {Id: "mark-x", Mark: "x"},
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
		Mark:    "a",
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

// TestApplyArrivals is which puts report an agent to list pull requests for: one not held
// before, or one whose branch or repository moved, and never a snapshot.
func TestApplyArrivals(t *testing.T) {
	t.Parallel()

	var world assemble.World

	assert.Equal(t, []string{newer}, world.Apply(put(agentRow(agent(newer)))), "an agent not held before arrives")
	assert.Nil(t, world.Apply(put(agentRow(agent(newer)))), "the same agent again does not")
	moved := agent(newer, func(a *agentv1.Agent) { a.Branch = xptr.Ptr("elsewhere") })
	assert.Equal(t, []string{newer}, world.Apply(put(agentRow(moved))), "an agent whose branch moved arrives again")

	archivedAt := "2026-09-23T10:00:00Z"
	archived := agent(newer)
	archived.Branch = moved.Branch
	archived.ArchivedAt = &archivedAt
	assert.Nil(t, world.Apply(put(agentRow(archived))), "an agent being archived does not arrive")
	assert.Equal(t, []string{newer}, world.Apply(put(agentRow(moved))), "and one coming back from the archive does")
	assert.Nil(t, world.Apply(put(&watchv1.Row{Row: &watchv1.Row_Mark{Mark: &agentv1.Mark{Id: "mark-a", Mark: "a"}}})), "no other row arrives")

	snapshot := &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{
		Rows: []*watchv1.Row{agentRow(agent(older))},
	}}}
	assert.Nil(t, world.Apply(snapshot), "a snapshot carries its pull requests, so nothing arrives")
	assert.Equal(t, map[string]*agentv1.Agent{older: agent(older)}, world.Agents, "a snapshot replaces the world")
	assert.Empty(t, world.Marks, "a snapshot keeps no row it does not carry")

	assert.Nil(t, world.Apply(&watchv1.WatchEvent{Event: &watchv1.WatchEvent_Heartbeat{}}))
	assert.Nil(t, world.Apply(nil))
}
