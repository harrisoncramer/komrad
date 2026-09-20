//go:build integration

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"weave/internal/forge"
	"weave/internal/forge/github"
	"weave/internal/forge/gitlab"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	forgev1 "weave/proto/gen/forge/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeForge is a forge answering a fixed pair of pull requests from one branch of one
// repository, counting how often it was asked for the open one's review decision.
type fakeForge struct {
	server    *httptest.Server
	decisions atomic.Int32
}

// TestPullRequestsReachTheRowFromEachForge covers the whole road from a forge to the panel
// on a real checkout: the connection, the cwd loop reading the branch and origin, the sync
// storing the branch's pull requests and reading the open one's decision once, and both the
// listing and the record answering with where it stands.
func TestPullRequestsReachTheRowFromEachForge(t *testing.T) {
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
			fake:   fakeGitHub,
			build: func(server *httptest.Server) forge.Builder {
				return func(_ string, auth forge.Authenticator) forge.Forge {
					return github.New(github.NewParams{
						Auth: auth,
						API:  server.URL,
						Next: server.Client().Transport,
					})
				}
			},
		},
		{
			name:   "gitlab",
			host:   "gitlab.com",
			remote: "git@gitlab.com:acme/widgets.git",
			fake:   fakeGitLab,
			build: func(server *httptest.Server) forge.Builder {
				return func(_ string, auth forge.Authenticator) forge.Forge {
					return gitlab.New(gitlab.NewParams{
						Auth: auth,
						API:  server.URL,
						Next: server.Client().Transport,
					})
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hermeticGit(t)

			fake := tt.fake(t)
			kind, err := forge.KindForHost(tt.host)
			require.NoError(t, err)
			instance := weavetest.Start(t, weavetest.Params{
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

			pullRequests, err := instance.PullRequests.ListAgentPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListAgentPullRequestsRequest{AgentId: agentID}))
			require.NoError(t, err)
			assert.NotNil(t, pullRequests.Msg.RepositoryReadAt)
			require.Len(t, pullRequests.Msg.GetPullRequests(), 2)
			assert.Equal(t, int32(12), pullRequests.Msg.GetPullRequests()[0].GetNumber())
			assert.True(t, pullRequests.Msg.GetPullRequests()[0].GetCurrent())
			assert.Equal(t, int32(9), pullRequests.Msg.GetPullRequests()[1].GetNumber())
			assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED, pullRequests.Msg.GetPullRequests()[1].GetState())
			assert.False(t, pullRequests.Msg.GetPullRequests()[1].GetCurrent())

			instance.SyncPullRequests()
			assert.Equal(t, int32(1), fake.decisions.Load(), "a decision read since the last change is not read again")
		})
	}
}

// fakeGitHub answers GitHub's REST listing and GraphQL decision for two pull requests from
// feature/widgets of acme/widgets: #12 open and approved, #9 merged.
func fakeGitHub(t *testing.T) *fakeForge {
	t.Helper()

	fake := &fakeForge{}
	repository := map[string]any{"html_url": "https://github.com/acme/widgets"}
	pullRequest := func(number int, title, state string, merged bool) map[string]any {
		var mergedAt, closedAt any
		if merged {
			mergedAt, closedAt = "2026-09-18T12:00:00Z", "2026-09-18T12:00:00Z"
		}

		return map[string]any{
			"node_id":    fmt.Sprintf("PR_%d", number),
			"html_url":   fmt.Sprintf("https://github.com/acme/widgets/pull/%d", number),
			"number":     number,
			"title":      title,
			"state":      state,
			"draft":      false,
			"created_at": fmt.Sprintf("2026-09-%02dT10:00:00Z", number),
			"updated_at": fmt.Sprintf("2026-09-%02dT11:00:00Z", number),
			"merged_at":  mergedAt,
			"closed_at":  closedAt,
			"head":       map[string]any{"ref": "feature/widgets", "sha": "aaa", "repo": repository},
			"base":       map[string]any{"ref": "main", "sha": "bbb", "repo": repository},
		}
	}
	listing := []map[string]any{
		pullRequest(12, "ship the widgets", "open", false),
		pullRequest(9, "first cut", "closed", true),
	}

	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			writeJSON(t, w, map[string]any{"login": "harrisoncramer"})
		case "/repos/acme/widgets/pulls":
			writeJSON(t, w, listing)
		case "/graphql":
			fake.decisions.Add(1)
			writeJSON(t, w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewDecision": "APPROVED"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.server.Close)

	return fake
}

// fakeGitLab answers GitLab's REST listing, reviewers and approvals for two merge requests
// from feature/widgets of acme/widgets: !12 open and approved, !9 merged.
func fakeGitLab(t *testing.T) *fakeForge {
	t.Helper()

	fake := &fakeForge{}
	mergeRequest := func(iid int, title, state string, merged bool) map[string]any {
		var mergedAt any
		if merged {
			mergedAt = "2026-09-18T12:00:00Z"
		}

		return map[string]any{
			"id":                9000 + iid,
			"iid":               iid,
			"web_url":           fmt.Sprintf("https://gitlab.com/acme/widgets/-/merge_requests/%d", iid),
			"title":             title,
			"state":             state,
			"draft":             false,
			"created_at":        fmt.Sprintf("2026-09-%02dT10:00:00Z", iid),
			"updated_at":        fmt.Sprintf("2026-09-%02dT11:00:00Z", iid),
			"merged_at":         mergedAt,
			"closed_at":         nil,
			"source_branch":     "feature/widgets",
			"target_branch":     "main",
			"sha":               "aaa",
			"source_project_id": 42,
			"target_project_id": 42,
		}
	}
	listing := []map[string]any{
		mergeRequest(12, "ship the widgets", "opened", false),
		mergeRequest(9, "first cut", "merged", true),
	}

	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		switch {
		case path == "/user":
			writeJSON(t, w, map[string]any{"username": "harrisoncramer"})
		case path == "/projects/acme%2Fwidgets/merge_requests":
			writeJSON(t, w, listing)
		case strings.HasSuffix(path, "/merge_requests/12/reviewers"):
			fake.decisions.Add(1)
			writeJSON(t, w, []map[string]any{{"user": map[string]any{"id": 1}, "state": "approved"}})
		case strings.HasSuffix(path, "/merge_requests/12/approvals"):
			writeJSON(t, w, map[string]any{"approvals_required": 1, "approvals_left": 0, "approved_by": []map[string]any{{"user": map[string]any{"id": 1}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.server.Close)

	return fake
}

func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(body))
}
