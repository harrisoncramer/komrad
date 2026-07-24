package store

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertAndGetTranscript(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	got, err := st.GetTranscript(ctx, "missing")
	require.NoError(t, err)
	assert.Equal(t, Transcript{}, got, "an unsynced session reads as the zero transcript")

	require.NoError(t, st.UpsertTranscript(ctx, "sid", "hello world", 11))
	got, err = st.GetTranscript(ctx, "sid")
	require.NoError(t, err)
	assert.Equal(t, "hello world", got.Content)
	assert.Equal(t, int64(11), got.SyncedOffset)

	require.NoError(t, st.UpsertTranscript(ctx, "sid", "hello world again", 17))
	got, err = st.GetTranscript(ctx, "sid")
	require.NoError(t, err)
	assert.Equal(t, "hello world again", got.Content, "upsert replaces content in place")
	assert.Equal(t, int64(17), got.SyncedOffset)
}

// seedSession inserts a session and returns its generated id, so a transcript
// can be keyed to a real chat the search join can resolve.
func seedSession(t *testing.T, st *Store, name string) string {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, Agent{Name: name, State: StateNew}))
	got, err := st.Get(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}

func TestSearchTranscripts(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	one := seedSession(t, st, "one")
	two := seedSession(t, st, "two")
	require.NoError(t, st.UpsertTranscript(ctx, one, "the quick brown fox discusses postgres indexing", 1))
	require.NoError(t, st.UpsertTranscript(ctx, two, "an unrelated chat about breakfast cereal", 1))

	hits, err := st.SearchTranscripts(ctx, "postgres", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1, "only the matching transcript is returned")
	assert.Equal(t, one, hits[0].SessionID)
	assert.Equal(t, "one", hits[0].Name, "the hit carries the chat name")
	assert.False(t, hits[0].Deleted)
	assert.Contains(t, hits[0].Snippet, SnippetOpen+"postgres"+SnippetClose, "the match is marked in the snippet")

	hits, err = st.SearchTranscripts(ctx, "ostgr", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1, "trigram tokenizer matches on a substring, not just whole words")
	assert.Equal(t, one, hits[0].SessionID)
}

func TestSearchTranscriptsSpansDeleted(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	one := seedSession(t, st, "one")
	require.NoError(t, st.UpsertTranscript(ctx, one, "a chat that mentions postgres then is deleted", 1))
	require.NoError(t, st.SoftDelete(ctx, "one"))

	hits, err := st.SearchTranscripts(ctx, "postgres", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1, "a soft-deleted chat still turns up in search")
	assert.Equal(t, one, hits[0].SessionID)
	assert.True(t, hits[0].Deleted, "the hit is flagged deleted")
}

func TestSearchTranscriptsBlankQuery(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	one := seedSession(t, st, "one")
	require.NoError(t, st.UpsertTranscript(ctx, one, "anything at all", 1))

	hits, err := st.SearchTranscripts(ctx, "   ", 10)
	require.NoError(t, err)
	assert.Empty(t, hits, "a blank query returns nothing rather than every transcript")
}

func TestSearchTranscriptsReindexOnUpsert(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	one := seedSession(t, st, "one")
	require.NoError(t, st.UpsertTranscript(ctx, one, "talking about postgres", 1))
	require.NoError(t, st.UpsertTranscript(ctx, one, "now talking about sqlite instead", 2))

	hits, err := st.SearchTranscripts(ctx, "postgres", 10)
	require.NoError(t, err)
	assert.Empty(t, hits, "the old content is dropped from the index on re-upsert")

	hits, err = st.SearchTranscripts(ctx, "sqlite", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, one, hits[0].SessionID)
}

func TestFTSPhrase(t *testing.T) {
	assert.Equal(t, `"plain"`, ftsPhrase("plain"))
	assert.Equal(t, `"say ""hi"""`, ftsPhrase(`say "hi"`), "embedded quotes are doubled")
}

func TestSearchTranscriptsQueryWithOperators(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	one := seedSession(t, st, "one")
	require.NoError(t, st.UpsertTranscript(ctx, one, `the config uses a "flags" field and an OR clause`, 1))

	// A query carrying FTS operator syntax and quotes must be matched literally,
	// not parsed, so it neither errors nor mis-matches.
	hits, err := st.SearchTranscripts(ctx, `"flags" field`, 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, one, hits[0].SessionID)
	assert.True(t, strings.Contains(hits[0].Snippet, SnippetOpen), "the literal phrase is found and marked")
}

// TestDeleteRemovesEverythingKeyedToTheSession is the regression test for a
// half-delete: the session row went, but the transcript, its search index entry,
// and the config row stayed. The text remained readable through the transcript
// and the SQL tools while search no longer found it.
func TestDeleteRemovesEverythingKeyedToTheSession(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	require.NoError(t, st.Upsert(ctx, Agent{Name: "doomed", Cwd: "/tmp"}))
	agent, err := st.Get(ctx, "doomed")
	require.NoError(t, err)
	require.NotNil(t, agent)
	require.NoError(t, st.UpsertTranscript(ctx, agent.ID, "the text that should not survive", 0))

	deleted, err := st.Delete(ctx, "doomed")
	require.NoError(t, err)
	assert.True(t, deleted)

	transcript, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Empty(t, transcript.Content, "the transcript goes with the session")

	hits, err := st.SearchTranscripts(ctx, "should not survive", 10)
	require.NoError(t, err)
	assert.Empty(t, hits, "the search index goes with it too")

	var configs int
	require.NoError(t, st.conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM session_configs WHERE id = ?", agent.SessionConfigID).Scan(&configs))
	assert.Zero(t, configs, "the config row is not left orphaned")
}

// TestDeleteReportsWhetherAnythingWasThere confirms a delete of a name nobody
// has is distinguishable from a real one, so the RPC can answer not_found.
func TestDeleteReportsWhetherAnythingWasThere(t *testing.T) {
	st := open(t)

	deleted, err := st.Delete(context.Background(), "never-existed")
	require.NoError(t, err)
	assert.False(t, deleted)
}
