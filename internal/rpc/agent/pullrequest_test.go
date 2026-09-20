package agent

import (
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	pullrequeststore "weave/internal/store/pullrequest"
	"weave/internal/storetest"
	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const weaveRepository = "https://github.com/harrisoncramer/weave"

// TestListedAgentsCarryWhereTheirPullRequestStands pins the standing the listing gathers from
// the sync's tables: unread until the repository has been read, none once it has and the branch
// holds nothing, and otherwise the current pull request with its state, number, address and
// title.
func TestListedAgentsCarryWhereTheirPullRequestStands(t *testing.T) {
	svc, st := service(t)
	ctx := t.Context()

	unread := seedAgentOn(t, st, "unread", "https://github.com/harrisoncramer/unread", "agent/x")
	none := seedAgentOn(t, st, "none", weaveRepository, "agent/none")
	opened := seedAgentOn(t, st, "opened", weaveRepository, "agent/x")
	local := st.SeedAgent(t, agentstore.Agent{Name: "local"})

	require.NoError(t, st.PullRequests.EnsureSync(ctx, weaveRepository))
	require.NoError(t, st.PullRequests.RecordSyncRequest(ctx, weaveRepository))

	openedAt := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	written, err := st.PullRequests.Upsert(ctx, pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        "PR_740",
		URL:               "https://github.com/harrisoncramer/weave/pull/740",
		RepositoryURL:     weaveRepository,
		HeadRepositoryURL: weaveRepository,
		Number:            740,
		Title:             "sync pull requests from the forges",
		HeadBranch:        "agent/x",
		HeadSHA:           "abc",
		BaseBranch:        "main",
		BaseSHA:           "def",
		State:             "open",
		OpenedAt:          openedAt,
		ChangedAt:         openedAt,
	})
	require.NoError(t, err)
	require.NoError(t, st.PullRequests.SetReviewDecision(ctx, written.ID, "CHANGES_REQUESTED"))

	listed, err := svc.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{}))
	require.NoError(t, err)
	standings := map[string]*agentv1.PullRequestStanding{}
	for _, agent := range listed.Msg.GetAgents() {
		standings[agent.GetId()] = agent.GetPullRequest()
	}

	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD, standings[unread].GetState())
	assert.Nil(t, standings[unread].Number)
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD, standings[local.ID].GetState(), "a checkout off any forge is never read")
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE, standings[none].GetState())
	assert.Nil(t, standings[none].Url)

	current := standings[opened]
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED, current.GetState())
	assert.Equal(t, int32(740), current.GetNumber())
	assert.Equal(t, "https://github.com/harrisoncramer/weave/pull/740", current.GetUrl())
	assert.Equal(t, "sync pull requests from the forges", current.GetTitle())

	got, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: opened}))
	require.NoError(t, err)
	assert.Equal(t, int32(740), got.Msg.GetAgent().GetPullRequest().GetNumber(), "one agent carries the same standing as the listing")
}

// seedAgentOn is a live agent whose checkout is on a branch of a repository.
func seedAgentOn(t *testing.T, st *storetest.Stores, name, repositoryURL, branch string) string {
	t.Helper()
	ctx := t.Context()

	id := st.SeedAgent(t, agentstore.Agent{Name: name}).ID
	require.NoError(t, st.Agents.SetRepositoryURLByID(ctx, id, &repositoryURL))
	require.NoError(t, st.Agents.SetBranchByID(ctx, id, &branch))

	return id
}
