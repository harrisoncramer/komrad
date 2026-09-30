package pullrequest

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var threadWrittenAt = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func threadOn(providerID string, comments ...forge.ReviewComment) forge.ReviewThread {
	return forge.ReviewThread{
		ProviderID: providerID,
		Path:       "main.go",
		Line:       3,
		URL:        "https://github.com/harrisoncramer/komrad/pull/1#discussion_r" + providerID,
		Comments:   comments,
	}
}

func commentBy(providerID, login string) forge.ReviewComment {
	return forge.ReviewComment{ProviderID: providerID, Login: login, Body: "rename this", WrittenAt: threadWrittenAt}
}

func (h *harness) linkedPullRequest(t *testing.T, providerID string, number int, branch string) string {
	t.Helper()
	ctx := t.Context()

	_, err := h.Upsert(ctx, openPullRequest(providerID, number, branch))
	require.NoError(t, err)
	_, err = h.Observe(ctx)
	require.NoError(t, err)

	stored, err := h.GetByIdentity(ctx, "github.com", providerID)
	require.NoError(t, err)

	return stored.ID
}

// TestListCommentSyncTargets covers which pull requests the comment sync reads: open ones, drafts
// included, linked to an agent that is not archived.
func TestListCommentSyncTargets(t *testing.T) {
	h := open(t)
	ctx := t.Context()
	threads := NewForgeThreadStore(h.pool.GetDB())

	workflow := h.createWorkflow(t)
	h.createAgent(t, workflow, "live", komrad, "agent/x")
	archived := h.createAgent(t, workflow, "archived", komrad, "agent/y")
	_, err := h.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)

	_, err = h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	draft := openPullRequest("PR_2", 2, "agent/x")
	draft.Draft = true
	_, err = h.Upsert(ctx, draft)
	require.NoError(t, err)
	_, err = h.Upsert(ctx, finishedPullRequest("PR_3", 3, "agent/x"))
	require.NoError(t, err)
	_, err = h.Upsert(ctx, openPullRequest("PR_4", 4, "agent/y"))
	require.NoError(t, err)
	_, err = h.Upsert(ctx, openPullRequest("PR_5", 5, "agent/nobody"))
	require.NoError(t, err)
	_, err = h.Observe(ctx)
	require.NoError(t, err)

	targets, err := threads.ListCommentSyncTargets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, xslice.Map(targets, func(target CommentSyncTarget) int { return target.Number }))
	assert.Equal(t, komrad, targets[0].RepositoryURL)
}

// TestSetThreads covers what one write of the forge's answer leaves stored.
func TestSetThreads(t *testing.T) {
	first := []forge.ReviewThread{
		threadOn("T_1", commentBy("C_1", "dana"), commentBy("C_2", "harrisoncramer")),
		threadOn("T_2", commentBy("C_3", "lee")),
	}

	tests := []struct {
		name  string
		then  SetThreadsParams
		check func(t *testing.T, stored []ForgeThread)
	}{
		{
			name: "the viewer's comments are marked yours",
			check: func(t *testing.T, stored []ForgeThread) {
				require.Len(t, stored, 2)
				byID := map[string]ForgeThread{stored[0].ProviderID: stored[0], stored[1].ProviderID: stored[1]}
				require.Len(t, byID["T_1"].Comments, 2)
				assert.False(t, byID["T_1"].Comments[0].Yours)
				assert.True(t, byID["T_1"].Comments[1].Yours)
				assert.Equal(t, "main.go", byID["T_1"].Path)
				assert.Equal(t, 3, byID["T_1"].Line)
				assert.Equal(t, threadWrittenAt, byID["T_1"].WrittenAt.UTC())
			},
		},
		{
			name: "a pruning write removes what the forge no longer holds",
			then: SetThreadsParams{Threads: []forge.ReviewThread{threadOn("T_1", commentBy("C_1", "dana"))}, Prune: true},
			check: func(t *testing.T, stored []ForgeThread) {
				require.Len(t, stored, 1)
				assert.Equal(t, "T_1", stored[0].ProviderID)
				assert.Len(t, stored[0].Comments, 1)
			},
		},
		{
			name: "a pruning write of nothing removes every thread",
			then: SetThreadsParams{Threads: []forge.ReviewThread{}, Prune: true},
			check: func(t *testing.T, stored []ForgeThread) {
				assert.Empty(t, stored)
			},
		},
		{
			name: "a write that does not prune keeps what it left out",
			then: SetThreadsParams{Threads: []forge.ReviewThread{threadOn("T_1", commentBy("C_1", "dana"))}},
			check: func(t *testing.T, stored []ForgeThread) {
				require.Len(t, stored, 2)
			},
		},
		{
			name: "a whole-change thread stores no line",
			then: SetThreadsParams{Threads: []forge.ReviewThread{{ProviderID: "R_1", Comments: []forge.ReviewComment{commentBy("R_1", "dana")}}}},
			check: func(t *testing.T, stored []ForgeThread) {
				require.Len(t, stored, 3)
				for _, thread := range stored {
					if thread.ProviderID == "R_1" {
						assert.Empty(t, thread.Path)
						assert.Zero(t, thread.Line)
					}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			ctx := t.Context()
			threads := NewForgeThreadStore(h.pool.GetDB())

			h.createAgent(t, h.createWorkflow(t), "live", komrad, "agent/x")
			pullRequestID := h.linkedPullRequest(t, "PR_1", 1, "agent/x")

			require.NoError(t, threads.SetThreads(ctx, SetThreadsParams{
				PullRequestID: pullRequestID,
				Viewer:        "harrisoncramer",
				Threads:       first,
				Prune:         true,
			}))
			if tt.then.Threads != nil {
				tt.then.PullRequestID = pullRequestID
				tt.then.Viewer = "harrisoncramer"
				require.NoError(t, threads.SetThreads(ctx, tt.then))
			}

			stored, err := threads.ListWatched(ctx, nil)
			require.NoError(t, err)
			tt.check(t, stored)
		})
	}
}
