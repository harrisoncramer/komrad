//go:build integration

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"komrade/internal/forge"
	"komrade/internal/forge/github"
	"komrade/internal/forge/gitlab"
	"komrade/internal/komradetest"
	forgev1 "komrade/proto/gen/forge/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// forgeTokenEnv names a GitHub token that reads harrisoncramer/komrade-forge-fixture, and the
// live test skips without one so the suite never needs the network.
const forgeTokenEnv = "KOMRADE_E2E_GITHUB_TOKEN"

// fakeForge is a forge answering a fixed pair of pull requests from one branch of one
// repository, counting how often it was asked for the open one's review decision.
type fakeForge struct {
	server    *httptest.Server
	standings atomic.Int32
}

// TestIntegrationForges walks a person connecting a forge, using it, and disconnecting it.
func TestIntegrationForges(t *testing.T) {
	fake := startFakeGitHub(t)
	instance := komradetest.Start(t, komradetest.Params{
		ForgeBuilders: map[string]forge.Builder{forge.KindGitHub: buildGitHub(fake.server)},
	})
	ctx := t.Context()

	t.Run("connecting proves the token and records who it belongs to", func(t *testing.T) {
		connected, err := instance.Forges.ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{Host: "github.com", Token: "ghp_secret"}))
		require.NoError(t, err)
		assert.Equal(t, "harrisoncramer", connected.Msg.GetForge().GetLogin())
		assert.True(t, connected.Msg.GetForge().GetHasCredential())
	})

	t.Run("a connected host cannot be connected again", func(t *testing.T) {
		_, err := instance.Forges.ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{Host: "github.com", Token: "ghp_other"}))
		assert.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))
	})

	t.Run("the listing never carries the token", func(t *testing.T) {
		listed, err := instance.Forges.ListForges(ctx, connect.NewRequest(&emptypb.Empty{}))
		require.NoError(t, err)
		require.Len(t, listed.Msg.GetForges(), 1)
		assert.NotContains(t, listed.Msg.String(), "ghp_secret")
	})

	t.Run("a check finds nothing wrong", func(t *testing.T) {
		checked, err := instance.Forges.CheckForges(ctx, connect.NewRequest(&emptypb.Empty{}))
		require.NoError(t, err)
		require.Len(t, checked.Msg.GetChecks(), 1)
		assert.Empty(t, checked.Msg.GetChecks()[0].GetProblem())
	})

	t.Run("a branch's pull requests are read from the forge", func(t *testing.T) {
		pullRequests, err := instance.Forges.ListForgePullRequests(ctx, connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
			RemoteUrl: "git@github.com:acme/widgets.git",
			Branch:    "feature/widgets",
		}))
		require.NoError(t, err)
		require.Len(t, pullRequests.Msg.GetPullRequests(), 2)
		assert.Equal(t, int32(12), pullRequests.Msg.GetPullRequests()[0].GetNumber())
		assert.Nil(t, pullRequests.Msg.GetPullRequests()[0].MergedAt)
		assert.Equal(t, int32(9), pullRequests.Msg.GetPullRequests()[1].GetNumber())
		assert.NotNil(t, pullRequests.Msg.GetPullRequests()[1].MergedAt)
	})

	t.Run("a host nothing is connected to is not found", func(t *testing.T) {
		_, err := instance.Forges.ListForgePullRequests(ctx, connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
			RemoteUrl: "git@gitlab.com:acme/widgets.git",
			Branch:    "main",
		}))
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("disconnecting takes the connection away", func(t *testing.T) {
		_, err := instance.Forges.DisconnectForge(ctx, connect.NewRequest(&forgev1.DisconnectForgeRequest{Host: "github.com"}))
		require.NoError(t, err)

		listed, err := instance.Forges.ListForges(ctx, connect.NewRequest(&emptypb.Empty{}))
		require.NoError(t, err)
		assert.Empty(t, listed.Msg.GetForges())
	})
}

// TestIntegrationForgeSpeaksToGitHub connects the real GitHub and reads a pull request through it.
func TestIntegrationForgeSpeaksToGitHub(t *testing.T) {
	token := os.Getenv(forgeTokenEnv)
	if token == "" {
		t.Skipf("%s is not set", forgeTokenEnv)
	}

	instance := komradetest.Start(t, komradetest.Params{})
	ctx := t.Context()

	connected, err := instance.Forges.ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{Host: "github.com", Token: token}))
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", connected.Msg.GetForge().GetLogin())

	checked, err := instance.Forges.CheckForges(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.Len(t, checked.Msg.GetChecks(), 1)
	assert.Empty(t, checked.Msg.GetChecks()[0].GetProblem())

	pullRequests, err := instance.Forges.ListForgePullRequests(ctx, connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
		RemoteUrl: "git@github.com:harrisoncramer/komrade-forge-fixture.git",
		Branch:    "fixture/open",
	}))
	require.NoError(t, err)
	require.Len(t, pullRequests.Msg.GetPullRequests(), 1)
	assert.Equal(t, int32(1), pullRequests.Msg.GetPullRequests()[0].GetNumber())
	assert.Nil(t, pullRequests.Msg.GetPullRequests()[0].MergedAt)
}

func buildGitHub(server *httptest.Server) forge.Builder {
	return func(_ string, auth forge.Authenticator) forge.Forge {
		return github.NewForge(github.NewForgeParams{
			Auth: auth,
			API:  server.URL,
			Next: server.Client().Transport,
		})
	}
}

func buildGitLab(server *httptest.Server) forge.Builder {
	return func(_ string, auth forge.Authenticator) forge.Forge {
		return gitlab.NewForge(gitlab.NewForgeParams{
			Auth: auth,
			API:  server.URL,
			Next: server.Client().Transport,
		})
	}
}

// readOperation is the name a posted GraphQL query was written with.
func readOperation(r *http.Request) string {
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

// buildConnection is a page of nodes as either forge's GraphQL answers it.
func buildConnection(nodes ...map[string]any) map[string]any {
	if nodes == nil {
		nodes = []map[string]any{}
	}

	return map[string]any{
		"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
		"nodes":    nodes,
	}
}

// startFakeGitHub answers GitHub's GraphQL for two pull requests from feature/widgets of
// acme/widgets: #12 open and approved, #9 merged, and a search that never answers.
func startFakeGitHub(t *testing.T) *fakeForge {
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
	listing := buildConnection(
		pullRequest(12, "ship the widgets", "OPEN", false),
		pullRequest(9, "first cut", "MERGED", true),
	)

	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)

			return
		}

		switch readOperation(r) {
		case "Verify":
			writeJSON(t, w, map[string]any{"data": map[string]any{"viewer": map[string]any{"login": "harrisoncramer"}}})
		case "ListUpdated", "List":
			writeJSON(t, w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequests": listing}}})
		case "ListChanged":
			writeJSON(t, w, map[string]any{"data": map[string]any{"search": buildConnection()}})
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

// startFakeGitLab answers GitLab's GraphQL for two merge requests from feature/widgets of
// acme/widgets: !12 open and approved, !9 merged, and no changes since.
func startFakeGitLab(t *testing.T) *fakeForge {
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
	listing := buildConnection(
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

		switch readOperation(r) {
		case "Verify":
			writeJSON(t, w, map[string]any{"data": map[string]any{"currentUser": map[string]any{"username": "harrisoncramer"}}})
		case "ListUpdated", "List":
			writeJSON(t, w, map[string]any{"data": map[string]any{"project": map[string]any{"mergeRequests": listing}}})
		case "ListChanged":
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"r0":              map[string]any{"mergeRequests": buildConnection()},
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
