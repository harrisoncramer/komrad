//go:build integration

package e2e

import (
	"net/http/httptest"
	"testing"

	"komrad/internal/forge"
	"komrad/internal/komradtest"
	agentv1 "komrad/proto/gen/agent/v1"
	forgev1 "komrad/proto/gen/forge/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationPullRequestsReachTheRowFromEachForge covers the whole road from a forge to the panel
// on a real checkout: the connection, the cwd loop reading the branch and origin, the sync
// storing the branch's pull requests and reading the open one's decision once, and both the
// listing and the record answering with where it stands.
func TestIntegrationPullRequestsReachTheRowFromEachForge(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		remote string
		fake   func(t *testing.T) *fakeForge
		build  func(server *httptest.Server) forge.Builder
	}{
		{
			name:   "github",
			host:   "github.com",
			remote: "git@github.com:acme/widgets.git",
			fake:   startFakeGitHub,
			build:  buildGitHub,
		},
		{
			name:   "gitlab",
			host:   "gitlab.com",
			remote: "git@gitlab.com:acme/widgets.git",
			fake:   startFakeGitLab,
			build:  buildGitLab,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateGit(t)

			fake := tt.fake(t)
			kind, err := forge.KindForHost(tt.host)
			require.NoError(t, err)
			instance := komradtest.Start(t, komradtest.Params{
				WorktreesDir:  t.TempDir(),
				ForgeBuilders: map[string]forge.Builder{kind: tt.build(fake.server)},
			})
			ctx := t.Context()

			connected, err := instance.Forges.ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{Host: tt.host, Token: "secret"}))
			require.NoError(t, err)
			assert.Equal(t, "harrisoncramer", connected.Msg.GetForge().GetLogin())

			repo := initGitRepo(t)
			runGitIn(t, repo, "remote", "set-url", "origin", tt.remote)
			runGitIn(t, repo, "checkout", "--quiet", "-b", "feature/widgets")

			workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
				Name: "widgets",
				Cwd:  repo,
			}))
			require.NoError(t, err)

			created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
				Prompt:     "ship the widgets",
				Title:      "widgets",
				WorkflowId: workflow.Msg.GetWorkflow().GetId(),
			}))
			require.NoError(t, err)
			agentID := created.Msg.GetAgent().GetId()

			instance.SyncCwds()
			instance.SyncPullRequests()

			listed, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agentID}))
			require.NoError(t, err)
			standing := listed.Msg.GetAgent().GetPullRequest()
			assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED, standing.GetState())
			assert.Equal(t, int32(12), standing.GetNumber())
			assert.Equal(t, "ship the widgets", standing.GetTitle())
			assert.Contains(t, standing.GetUrl(), "/12")

			onBranch := listed.Msg.GetAgent().GetPullRequests()
			require.Len(t, onBranch, 2)
			assert.Equal(t, int32(12), onBranch[0].GetNumber())
			assert.Equal(t, int32(9), onBranch[1].GetNumber())
			assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED, onBranch[1].GetState())

			pullRequests, err := instance.PullRequests.ListPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{RepositoryUrl: tt.remote, HeadBranch: "feature/widgets"}))
			require.NoError(t, err)
			require.Len(t, pullRequests.Msg.GetPullRequests(), 2, "the row's pull requests are the stored listing for its branch")
			assert.Equal(t, onBranch[0].GetId(), pullRequests.Msg.GetPullRequests()[0].GetId())
			assert.Equal(t, onBranch[1].GetId(), pullRequests.Msg.GetPullRequests()[1].GetId())

			instance.SyncPullRequests()
			assert.Equal(t, int32(1), fake.standings.Load(), "a standing read since the last change is not read again")
		})
	}
}
