//go:build integration

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	standings atomic.Int32
}

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
			fake:   fakeGitHub,
			build: func(server *httptest.Server) forge.Builder {
				return func(_ string, auth forge.Authenticator) forge.Forge {
					return github.NewForge(github.NewForgeParams{
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
					return gitlab.NewForge(gitlab.NewForgeParams{
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
			isolateGit(t)

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

// operationOf is the name a posted GraphQL query was written with.
func operationOf(r *http.Request) string {
	var posted struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
		return ""
	}

	name, _, _ := strings.Cut(strings.TrimPrefix(posted.Query, "query "), "(")
	name, _, _ = strings.Cut(name, " ")

	return name
}

// connection is a page of nodes as either forge's GraphQL answers it.
func connection(nodes ...map[string]any) map[string]any {
	if nodes == nil {
		nodes = []map[string]any{}
	}

	return map[string]any{
		"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
		"nodes":    nodes,
	}
}

// fakeGitHub answers GitHub's GraphQL for two pull requests from feature/widgets of
// acme/widgets: #12 open and approved, #9 merged, and a search that never answers.
func fakeGitHub(t *testing.T) *fakeForge {
	t.Helper()

	fake := &fakeForge{}
	repository := map[string]any{"url": "https://github.com/acme/widgets"}
	pullRequest := func(number int, title, state string, merged bool) map[string]any {
		var mergedAt, closedAt any
		if merged {
			mergedAt, closedAt = "2026-09-18T12:00:00Z", "2026-09-18T12:00:00Z"
		}

		return map[string]any{
			"id":             fmt.Sprintf("PR_%d", number),
			"url":            fmt.Sprintf("https://github.com/acme/widgets/pull/%d", number),
			"number":         number,
			"title":          title,
			"state":          state,
			"isDraft":        false,
			"createdAt":      fmt.Sprintf("2026-09-%02dT10:00:00Z", number),
			"updatedAt":      fmt.Sprintf("2026-09-%02dT11:00:00Z", number),
			"mergedAt":       mergedAt,
			"closedAt":       closedAt,
			"headRefName":    "feature/widgets",
			"headRefOid":     "aaa",
			"baseRefName":    "main",
			"baseRefOid":     "bbb",
			"headRepository": repository,
			"baseRepository": repository,
		}
	}
	listing := connection(
		pullRequest(12, "ship the widgets", "OPEN", false),
		pullRequest(9, "first cut", "MERGED", true),
	)

	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)

			return
		}

		switch operationOf(r) {
		case "Verify":
			writeJSON(t, w, map[string]any{"data": map[string]any{"viewer": map[string]any{"login": "harrisoncramer"}}})
		case "ListUpdated", "List":
			writeJSON(t, w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequests": listing}}})
		case "ListChanged":
			writeJSON(t, w, map[string]any{"data": map[string]any{"search": connection()}})
		case "Standings":
			fake.standings.Add(1)
			writeJSON(t, w, map[string]any{"data": map[string]any{"repository": map[string]any{"p12": map[string]any{
				"headRefOid":       "aaa",
				"reviewDecision":   "APPROVED",
				"mergeStateStatus": "CLEAN",
				"commits":          map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{"statusCheckRollup": map[string]any{"state": "SUCCESS"}}}}},
			}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.server.Close)

	return fake
}

// fakeGitLab answers GitLab's GraphQL for two merge requests from feature/widgets of
// acme/widgets: !12 open and approved, !9 merged, and no changes since.
func fakeGitLab(t *testing.T) *fakeForge {
	t.Helper()

	fake := &fakeForge{}
	project := map[string]any{"webUrl": "https://gitlab.com/acme/widgets"}
	mergeRequest := func(iid int, title, state string, merged bool) map[string]any {
		var mergedAt any
		if merged {
			mergedAt = "2026-09-18T12:00:00Z"
		}

		return map[string]any{
			"id":            fmt.Sprintf("gid://gitlab/MergeRequest/%d", 9000+iid),
			"iid":           strconv.Itoa(iid),
			"webUrl":        fmt.Sprintf("https://gitlab.com/acme/widgets/-/merge_requests/%d", iid),
			"title":         title,
			"state":         state,
			"draft":         false,
			"createdAt":     fmt.Sprintf("2026-09-%02dT10:00:00Z", iid),
			"updatedAt":     fmt.Sprintf("2026-09-%02dT11:00:00Z", iid),
			"mergedAt":      mergedAt,
			"closedAt":      nil,
			"sourceBranch":  "feature/widgets",
			"targetBranch":  "main",
			"diffHeadSha":   "aaa",
			"sourceProject": project,
			"targetProject": project,
			"diffRefs":      map[string]any{"baseSha": "bbb"},
		}
	}
	listing := connection(
		mergeRequest(12, "ship the widgets", "opened", false),
		mergeRequest(9, "first cut", "merged", true),
	)
	approved := map[string]any{
		"approvalsRequired":   1,
		"approvalsLeft":       0,
		"approvedBy":          map[string]any{"count": 1},
		"reviewers":           map[string]any{"nodes": []map[string]any{{"mergeRequestInteraction": map[string]any{"reviewState": "APPROVED"}}}},
		"diffHeadSha":         "aaa",
		"headPipeline":        map[string]any{"status": "SUCCESS"},
		"detailedMergeStatus": "MERGEABLE",
	}

	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)

			return
		}

		switch operationOf(r) {
		case "Verify":
			writeJSON(t, w, map[string]any{"data": map[string]any{"currentUser": map[string]any{"username": "harrisoncramer"}}})
		case "ListUpdated", "List":
			writeJSON(t, w, map[string]any{"data": map[string]any{"project": map[string]any{"mergeRequests": listing}}})
		case "ListChanged":
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"r0":              map[string]any{"mergeRequests": connection()},
				"queryComplexity": map[string]any{"score": 52, "limit": 200},
			}})
		case "Standings":
			fake.standings.Add(1)
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"project":         map[string]any{"m12": approved},
				"queryComplexity": map[string]any{"score": 20, "limit": 200},
			}})
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
