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
	id := seedSession(t, st, "chat")

	got, err := st.GetTranscript(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, Transcript{}, got, "an unsynced session reads as the zero transcript")

	require.NoError(t, st.UpsertTranscript(ctx, id, "hello world", 11))
	got, err = st.GetTranscript(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "hello world", got.Content)
	assert.Equal(t, int64(11), got.SyncedOffset)

	require.NoError(t, st.UpsertTranscript(ctx, id, "hello world again", 17))
	got, err = st.GetTranscript(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "hello world again", got.Content, "upsert replaces content in place")
	assert.Equal(t, int64(17), got.SyncedOffset)
}

// TestGetTranscriptOfUnknownIDIsEmpty covers the two ways an id can fail to name
// a chat: one that is well-formed but unused, and one that is not an id at all.
// Both read as the zero transcript rather than an error, so a caller that
// mistyped an id gets an empty result instead of a failure.
func TestGetTranscriptOfUnknownIDIsEmpty(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	tests := []struct {
		name string
		id   string
	}{
		{
			name: "a malformed id is not a chat",
			id:   "missing",
		},
		{
			name: "a well-formed id nothing holds",
			id:   "00000000-0000-7000-8000-000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.GetTranscript(ctx, tt.id)
			require.NoError(t, err)
			assert.Equal(t, Transcript{}, got)
		})
	}
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

	hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "postgres", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 1, "only the matching transcript is returned")
	assert.Equal(t, one, hits[0].SessionID)
	assert.Equal(t, "one", hits[0].Name, "the hit carries the chat name")
	assert.False(t, hits[0].Deleted)
	assert.Contains(t, hits[0].Snippet, SnippetOpen+"postgres"+SnippetClose, "the match is marked in the snippet")

	hits, err = st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "ostgr", Limit: 10})
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

	hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "postgres", Limit: 10})
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

	hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "   ", Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, hits, "a blank query returns nothing rather than every transcript")
}

func TestSearchTranscriptsReindexOnUpsert(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	one := seedSession(t, st, "one")
	require.NoError(t, st.UpsertTranscript(ctx, one, "talking about postgres", 1))
	require.NoError(t, st.UpsertTranscript(ctx, one, "now talking about sqlite instead", 2))

	hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "postgres", Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, hits, "the old content is dropped from the index on re-upsert")

	hits, err = st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "sqlite", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, one, hits[0].SessionID)
}

func TestLikePattern(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "a wildcard is escaped so it cannot widen the search",
			query: "100%",
			want:  `%100\%%`,
		},
		{
			name:  "an underscore is escaped, since LIKE reads it as any character",
			query: "session_id",
			want:  `%session\_id%`,
		},
		{
			name:  "a backslash is escaped, since it is the escape character itself",
			query: `a\b`,
			want:  `%a\\b%`,
		},
		{
			name:  "ordinary text is only wrapped",
			query: "plain",
			want:  "%plain%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, likePattern(tt.query))
		})
	}
}

// TestSearchTranscriptsTreatsWildcardsLiterally is the behaviour the escaping
// above exists for: a query containing a LIKE wildcard must match that character
// and not stand in for any run of text.
func TestSearchTranscriptsTreatsWildcardsLiterally(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	literal := seedSession(t, st, "literal")
	other := seedSession(t, st, "other")
	require.NoError(t, st.UpsertTranscript(ctx, literal, "the coverage hit 100% today", 1))
	require.NoError(t, st.UpsertTranscript(ctx, other, "the coverage hit 100 of them today", 1))

	hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "100%", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 1, "the percent matches itself rather than any text")
	assert.Equal(t, literal, hits[0].SessionID)
}

// TestSearchTranscriptsRanksByOccurrences checks the ordering the page cursor
// depends on: the transcript mentioning the query most comes first.
func TestSearchTranscriptsRanksByOccurrences(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	once := seedSession(t, st, "once")
	thrice := seedSession(t, st, "thrice")
	require.NoError(t, st.UpsertTranscript(ctx, once, "we mentioned pgx here", 1))
	require.NoError(t, st.UpsertTranscript(ctx, thrice, "pgx, and pgx again, and pgx once more", 1))

	hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "pgx", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 2)
	assert.Equal(t, thrice, hits[0].SessionID)
	assert.EqualValues(t, 3, hits[0].Rank)
	assert.Equal(t, once, hits[1].SessionID)
	assert.EqualValues(t, 1, hits[1].Rank)
}

// TestSearchTranscriptsPagesByCursor walks a result set one hit at a time, which
// is what the RPC does when a caller follows the page tokens.
func TestSearchTranscriptsPagesByCursor(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	for _, name := range []string{"a", "b", "c"} {
		id := seedSession(t, st, name)
		require.NoError(t, st.UpsertTranscript(ctx, id, "shared term here", 1))
	}

	var (
		seen   []string
		cursor *TranscriptCursor
	)
	for range 3 {
		hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "shared", Limit: 1, Cursor: cursor})
		require.NoError(t, err)
		require.Len(t, hits, 1)

		seen = append(seen, hits[0].SessionID)
		cursor = &TranscriptCursor{Rank: hits[0].Rank, SessionID: hits[0].SessionID}
	}

	assert.Len(t, seen, 3)
	assert.Len(t, uniqueStrings(seen), 3, "each page returns a hit the previous pages did not")

	hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "shared", Limit: 1, Cursor: cursor})
	require.NoError(t, err)
	assert.Empty(t, hits, "the cursor past the last hit returns nothing")
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}

	return out
}

func TestSearchTranscriptsQueryWithOperators(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	one := seedSession(t, st, "one")
	require.NoError(t, st.UpsertTranscript(ctx, one, `the config uses a "flags" field and an OR clause`, 1))

	// A query carrying FTS operator syntax and quotes must be matched literally,
	// not parsed, so it neither errors nor mis-matches.
	hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: `"flags" field`, Limit: 10})
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

	hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "should not survive", Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, hits, "the search index goes with it too")

	var configs int
	require.NoError(t, st.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM session_configs WHERE id = $1", agent.SessionConfigID).Scan(&configs))
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

// TestSearchToleratesControlCharacters is the regression test for a NUL byte in
// a query: FTS5 re-parses the phrase and reads the NUL as the end of the string,
// so the closing quote vanished and an ordinary search became a 500.
func TestSearchToleratesControlCharacters(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	require.NoError(t, st.Upsert(ctx, Agent{Name: "chat"}))
	agent, err := st.Get(ctx, "chat")
	require.NoError(t, err)
	require.NotNil(t, agent)
	require.NoError(t, st.UpsertTranscript(ctx, agent.ID, "findable text", 0))

	hits, err := st.SearchTranscripts(ctx, SearchTranscriptsParams{Query: "find\x00able", Limit: 10})
	require.NoError(t, err, "a NUL in the query is not an error")
	assert.Len(t, hits, 1, "the NUL is dropped rather than truncating the phrase")
}
