package pullrequests

import (
	"testing"
	"time"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"

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
	f.forge.marks = map[int]string{1: "a", 2: "b", 3: "c", 4: "d"}
	f.forge.threads = map[int][]forge.ReviewThread{}

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

func lineThread(providerID, body string) forge.ReviewThread {
	return forge.ReviewThread{
		ProviderID: providerID,
		Path:       "internal/mail/send.go",
		Side:       forge.SideNew,
		CommitSHA:  scriptedHeadSHA,
		BaseSHA:    "def",
		EndLine:    88,
		DiffHunk:   "@@ -86,3 +86,3 @@\n if expiresAt.IsZero() {\n-\treturn nil\n+\treturn nil, errNoDeadline",
		Comments: []forge.ReviewComment{{
			ProviderID: providerID + "_c1",
			Login:      "dana",
			Body:       body,
			WrittenAt:  syncNow.Add(-time.Hour),
		}},
	}
}

// TestCommentSyncReadsOpenPullRequestsAgentsAreOn covers which pull requests a pass asks about.
func TestCommentSyncReadsOpenPullRequestsAgentsAreOn(t *testing.T) {
	f := newCommentFixture(t)
	f.forge.threads[1] = []forge.ReviewThread{lineThread("T_1", "fall back to the deadline?")}

	f.commentPass(t, syncNow)

	assert.Equal(t, []string{"marks:1,2", "threads:1", "threads:2"}, f.forge.requests, "drafts are read and merged or unowned pull requests are not")
	stored := f.stored(t)
	require.Len(t, stored, 1)
	assert.Equal(t, "internal/mail/send.go", stored[0].Path)
	assert.Equal(t, []string{"\treturn nil, errNoDeadline"}, stored[0].QuotedLines)
	require.Len(t, stored[0].Comments, 1)
	assert.Equal(t, "dana", stored[0].Comments[0].AuthorLogin)
}

// TestCommentSyncRereadsOnlyWhatMoved covers when a second pass reads threads again.
func TestCommentSyncRereadsOnlyWhatMoved(t *testing.T) {
	tests := []struct {
		name  string
		after time.Duration
		marks map[int]string
		want  []string
	}{
		{
			name:  "a mark holding still is not read again",
			after: time.Minute,
			marks: map[int]string{1: "a", 2: "b"},
			want:  []string{"marks:1,2"},
		},
		{
			name:  "a moved mark is read again",
			after: time.Minute,
			marks: map[int]string{1: "a2", 2: "b"},
			want:  []string{"marks:1,2", "threads:1"},
		},
		{
			name:  "a read gone stale is read again",
			after: commentRereadAfter,
			marks: map[int]string{1: "a", 2: "b"},
			want:  []string{"marks:1,2", "threads:1", "threads:2"},
		},
		{
			name:  "a pull request the forge does not answer for is left alone",
			after: time.Minute,
			marks: map[int]string{2: "b2"},
			want:  []string{"marks:1,2", "threads:2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCommentFixture(t)
			f.commentPass(t, syncNow)

			f.forge.marks = tt.marks
			f.commentPass(t, syncNow.Add(tt.after))

			assert.Equal(t, tt.want, f.forge.requests)
		})
	}
}

// TestCommentSyncReplacesWhatTheForgeHolds covers a reread overwriting the stored threads.
func TestCommentSyncReplacesWhatTheForgeHolds(t *testing.T) {
	f := newCommentFixture(t)
	kept := lineThread("T_1", "fall back to the deadline?")
	f.forge.threads[1] = []forge.ReviewThread{kept, lineThread("T_2", "rename this")}
	f.commentPass(t, syncNow)
	require.Len(t, f.stored(t), 2)

	kept.Resolved = true
	kept.Comments = append(kept.Comments, forge.ReviewComment{
		ProviderID: "T_1_c2",
		Login:      "lee",
		Body:       "agreed",
		WrittenAt:  syncNow,
	})
	f.forge.threads[1] = []forge.ReviewThread{kept}
	f.forge.marks[1] = "a2"
	f.commentPass(t, syncNow.Add(time.Minute))

	stored := f.stored(t)
	require.Len(t, stored, 1, "a thread the forge no longer holds is removed")
	assert.Equal(t, "T_1", stored[0].ProviderID)
	assert.True(t, stored[0].Resolved)
	assert.Equal(t, []string{"dana", "lee"}, []string{stored[0].Comments[0].AuthorLogin, stored[0].Comments[1].AuthorLogin})
}

// TestCommentSyncThrottlesTheConnection covers a rate limit refusal holding the host off.
func TestCommentSyncThrottlesTheConnection(t *testing.T) {
	f := newCommentFixture(t)
	f.forge.refuse = &forge.ThrottledError{}

	f.commentPass(t, syncNow)
	assert.Equal(t, []string{"marks:1,2"}, f.forge.requests)
	connection, err := f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	require.NotNil(t, connection.ThrottledUntil)
	assert.True(t, connection.ThrottledUntil.Equal(syncNow.Add(pullRequestThrottleFloor)))

	f.commentPass(t, syncNow.Add(30*time.Second))
	assert.Empty(t, f.forge.requests, "nothing is asked while the connection waits")
}
