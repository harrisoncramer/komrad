package pullrequests

import (
	"testing"
	"time"

	"komrad/internal/forge"
	forgestore "komrad/internal/store/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type commentFixture struct {
	*syncFixture
	comments *CommentSyncer
	threads  *pullrequeststore.ForgeThreadStore
}

func newCommentFixture(t *testing.T) *commentFixture {
	t.Helper()

	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	open := pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour))
	draft := pullRequestAt("PR_2", "agent/x", syncNow.Add(-2*time.Hour))
	draft.Draft = true
	merged := pullRequestAt("PR_3", "agent/x", syncNow.Add(-3*time.Hour))
	mergedAt := syncNow.Add(-3 * time.Hour)
	merged.MergedAt = &mergedAt
	merged.ClosedAt = &mergedAt
	merged.State = "MERGED"
	elsewhere := pullRequestAt("PR_4", "agent/nobody", syncNow.Add(-4*time.Hour))
	f.forge.updated[""] = &forge.Page{PullRequests: []forge.PullRequest{open, draft, merged, elsewhere}}
	f.sync(t)
	f.forge.requests = nil
	f.forge.threads = map[int]*forge.Threads{}

	threads := pullrequeststore.NewForgeThreadStore(f.stores.Pool.GetDB())
	scripted := f.forge

	return &commentFixture{
		syncFixture: f,
		threads:     threads,
		comments: NewCommentSyncer(NewCommentSyncerParams{
			Forges: forge.NewService(forge.NewServiceParams{
				Connections: f.connections,
				Credentials: forge.NewFileStore(t.TempDir()),
				Builders: map[string]forge.Builder{
					forge.KindGitHub: func(string, forge.Authenticator) forge.Forge {
						return scripted
					},
				},
			}),
			Threads: threads,
			Tx:      f.stores.Pool,
			Now:     func() time.Time { return f.now },
		}),
	}
}

func (f *commentFixture) commentPass(t *testing.T, at time.Time) {
	t.Helper()
	f.now = at
	f.forge.requests = nil
	require.NoError(t, f.comments.Sync(t.Context()))
}

func (f *commentFixture) stored(t *testing.T) []pullrequeststore.ForgeThread {
	t.Helper()
	threads, err := f.threads.ListWatched(t.Context(), nil)
	require.NoError(t, err)

	return threads
}

func lineThread(providerID, login string) forge.ReviewThread {
	return forge.ReviewThread{
		ProviderID: providerID,
		Path:       "internal/mail/send.go",
		Line:       88,
		Comments: []forge.ReviewComment{{
			ProviderID: providerID + "_c1",
			Login:      login,
			Body:       "fall back to the deadline?",
			WrittenAt:  syncNow.Add(-time.Hour),
		}},
	}
}

// TestCommentSyncReadsEachTargetAsTheConnection covers a pass asking about every target and
// marking the connection's own comments yours.
func TestCommentSyncReadsEachTargetAsTheConnection(t *testing.T) {
	f := newCommentFixture(t)
	f.forge.threads[1] = &forge.Threads{Threads: []forge.ReviewThread{lineThread("T_1", "dana"), lineThread("T_2", "harrisoncramer")}}

	f.commentPass(t, syncNow)

	assert.Equal(t, []string{"threads:1", "threads:2"}, f.forge.requests)
	yours := map[string]bool{}
	for _, thread := range f.stored(t) {
		yours[thread.ProviderID] = thread.Comments[0].Yours
	}
	assert.Equal(t, map[string]bool{"T_1": false, "T_2": true}, yours)
}

// TestCommentSyncKeepsThreadsATruncatedAnswerLeftOut covers an answer cut short never removing
// what it did not return.
func TestCommentSyncKeepsThreadsATruncatedAnswerLeftOut(t *testing.T) {
	f := newCommentFixture(t)
	f.forge.threads[1] = &forge.Threads{Threads: []forge.ReviewThread{lineThread("T_1", "dana"), lineThread("T_2", "lee")}}
	f.commentPass(t, syncNow)

	f.forge.threads[1] = &forge.Threads{Threads: []forge.ReviewThread{lineThread("T_1", "dana")}, Truncated: true}
	f.commentPass(t, syncNow.Add(time.Minute))
	assert.Len(t, f.stored(t), 2)

	f.forge.threads[1] = &forge.Threads{Threads: []forge.ReviewThread{lineThread("T_1", "dana")}}
	f.commentPass(t, syncNow.Add(2*time.Minute))
	assert.Len(t, f.stored(t), 1)
}

// TestCommentSyncThrottlesTheConnection covers a rate limit refusal holding the host off.
func TestCommentSyncThrottlesTheConnection(t *testing.T) {
	f := newCommentFixture(t)
	f.forge.refuse = &forge.ThrottledError{}

	f.commentPass(t, syncNow)
	assert.Equal(t, []string{"threads:1"}, f.forge.requests)
	connection, err := f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	require.NotNil(t, connection.ThrottledUntil)
	assert.True(t, connection.ThrottledUntil.Equal(syncNow.Add(pullRequestThrottleFloor)))

	f.commentPass(t, syncNow.Add(30*time.Second))
	assert.Empty(t, f.forge.requests, "nothing is asked while the connection waits")
}

// TestCommentSyncReadsInTurns covers a pass reading only a few pull requests, picking up where the
// last pass stopped.
func TestCommentSyncReadsInTurns(t *testing.T) {
	var targets []pullrequeststore.CommentSyncTarget
	for number := 1; number <= 7; number++ {
		targets = append(targets, pullrequeststore.CommentSyncTarget{Number: number})
	}
	numbers := func(turn []pullrequeststore.CommentSyncTarget) []int {
		return xslice.Map(turn, func(target pullrequeststore.CommentSyncTarget) int { return target.Number })
	}

	syncer := NewCommentSyncer(NewCommentSyncerParams{})
	assert.Equal(t, []int{1, 2, 3, 4, 5}, numbers(syncer.takeTurn(targets)))
	assert.Equal(t, []int{6, 7, 1, 2, 3}, numbers(syncer.takeTurn(targets)))
	assert.Equal(t, []int{4, 5, 6, 7, 1}, numbers(syncer.takeTurn(targets)))
	assert.Equal(t, []int{1, 2}, numbers(syncer.takeTurn(targets[:2])))
	assert.Equal(t, []int{1, 2, 3, 4, 5}, numbers(syncer.takeTurn(targets)))
}

// TestCommentSyncViewer covers whose comments count as the person's own on each kind of connection.
func TestCommentSyncViewer(t *testing.T) {
	tests := []struct {
		name       string
		connection forgestore.Connection
		want       string
	}{
		{
			name:       "a token reads as the login it was verified as",
			connection: forgestore.Connection{Method: forge.MethodToken, Login: "harrisoncramer"},
			want:       "harrisoncramer",
		},
		{
			name:       "an App reads as nobody, since its login is the App's",
			connection: forgestore.Connection{Method: forge.MethodGitHubApp, Login: "komrad-app"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, viewerOf(tt.connection))
		})
	}
}
