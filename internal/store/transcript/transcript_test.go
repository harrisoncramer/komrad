package transcript

import (
	"context"
	"strings"
	"testing"

	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"
	sessionstore "claude-agents/internal/store/session"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture is the transcript store under test plus the chat store the tests seed
// through. A transcript is keyed to a chat, and search joins the chats table for
// the name and archived flag, so a hit needs a real chat behind it. The two stay
// separate stores: this package never writes that table itself.
type fixture struct {
	*Store
	chats *sessionstore.Store
}

func open(t *testing.T) *fixture {
	t.Helper()
	p, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return &fixture{Store: New(p.DB()), chats: sessionstore.New(p.DB())}
}

func TestUpsertAndGetTranscript(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	id := seedSession(t, st, "chat")

	got, err := st.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, Transcript{}, got, "an unsynced session reads as the zero transcript")

	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: id, Content: "hello world", Offset: 11}))
	got, err = st.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "hello world", got.Content)
	assert.Equal(t, int64(11), got.SyncedOffset)

	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: id, Content: "hello world again", Offset: 17}))
	got, err = st.Get(ctx, id)
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
			got, err := st.Get(ctx, tt.id)
			require.NoError(t, err)
			assert.Equal(t, Transcript{}, got)
		})
	}
}

// seedSession inserts a session and returns its generated id, so a transcript
// can be keyed to a real chat the search join can resolve.
func seedSession(t *testing.T, st *fixture, name string) string {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.chats.Upsert(ctx, sessionstore.Agent{Name: name, State: sessionstore.StateNew}))
	got, err := st.chats.Get(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}

func TestSearchTranscripts(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	one := seedSession(t, st, "one")
	two := seedSession(t, st, "two")
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: one, Content: "the quick brown fox discusses postgres indexing", Offset: 1}))
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: two, Content: "an unrelated chat about breakfast cereal", Offset: 1}))

	hits, err := st.Search(ctx, SearchParams{Query: "postgres", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 1, "only the matching transcript is returned")
	assert.Equal(t, one, hits[0].SessionID)
	assert.Equal(t, "one", hits[0].Name, "the hit carries the chat name")
	assert.False(t, hits[0].Deleted)
	assert.Contains(t, hits[0].Snippet, SnippetOpen+"postgres"+SnippetClose, "the match is marked in the snippet")

	hits, err = st.Search(ctx, SearchParams{Query: "ostgr", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 1, "trigram tokenizer matches on a substring, not just whole words")
	assert.Equal(t, one, hits[0].SessionID)
}

func TestSearchTranscriptsSpansDeleted(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	one := seedSession(t, st, "one")
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: one, Content: "a chat that mentions postgres then is deleted", Offset: 1}))
	require.NoError(t, st.chats.Archive(ctx, "one"))

	hits, err := st.Search(ctx, SearchParams{Query: "postgres", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 1, "a soft-deleted chat still turns up in search")
	assert.Equal(t, one, hits[0].SessionID)
	assert.True(t, hits[0].Deleted, "the hit is flagged deleted")
}

func TestSearchTranscriptsBlankQuery(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	one := seedSession(t, st, "one")
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: one, Content: "anything at all", Offset: 1}))

	hits, err := st.Search(ctx, SearchParams{Query: "   ", Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, hits, "a blank query returns nothing rather than every transcript")
}

func TestSearchTranscriptsReindexOnUpsert(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	one := seedSession(t, st, "one")
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: one, Content: "talking about postgres", Offset: 1}))
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: one, Content: "now talking about sqlite instead", Offset: 2}))

	hits, err := st.Search(ctx, SearchParams{Query: "postgres", Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, hits, "the old content is dropped from the index on re-upsert")

	hits, err = st.Search(ctx, SearchParams{Query: "sqlite", Limit: 10})
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
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: literal, Content: "the coverage hit 100% today", Offset: 1}))
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: other, Content: "the coverage hit 100 of them today", Offset: 1}))

	hits, err := st.Search(ctx, SearchParams{Query: "100%", Limit: 10})
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
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: once, Content: "we mentioned pgx here", Offset: 1}))
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: thrice, Content: "pgx, and pgx again, and pgx once more", Offset: 1}))

	hits, err := st.Search(ctx, SearchParams{Query: "pgx", Limit: 10})
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
		require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: id, Content: "shared term here", Offset: 1}))
	}

	var (
		seen   []string
		cursor *Cursor
	)
	for range 3 {
		hits, err := st.Search(ctx, SearchParams{Query: "shared", Limit: 1, Cursor: cursor})
		require.NoError(t, err)
		require.Len(t, hits, 1)

		seen = append(seen, hits[0].SessionID)
		cursor = &Cursor{Rank: hits[0].Rank, SessionID: hits[0].SessionID}
	}

	assert.Len(t, seen, 3)
	assert.Len(t, uniqueStrings(seen), 3, "each page returns a hit the previous pages did not")

	hits, err := st.Search(ctx, SearchParams{Query: "shared", Limit: 1, Cursor: cursor})
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
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: one, Content: `the config uses a "flags" field and an OR clause`, Offset: 1}))

	// A query carrying FTS operator syntax and quotes must be matched literally,
	// not parsed, so it neither errors nor mis-matches.
	hits, err := st.Search(ctx, SearchParams{Query: `"flags" field`, Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, one, hits[0].SessionID)
	assert.True(t, strings.Contains(hits[0].Snippet, SnippetOpen), "the literal phrase is found and marked")
}

// TestSearchToleratesControlCharacters covers a NUL byte in a query. Postgres
// refuses one in a text value outright, so it is dropped before the query is
// sent and the search finds nothing rather than failing the request.
func TestSearchToleratesControlCharacters(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	id := seedSession(t, st, "chat")
	require.NoError(t, st.Upsert(ctx, UpsertParams{SessionID: id, Content: "findable text", Offset: 0}))

	hits, err := st.Search(ctx, SearchParams{Query: "find\x00able", Limit: 10})
	require.NoError(t, err, "a NUL in the query is not an error")
	assert.Len(t, hits, 1, "the NUL is dropped rather than truncating the phrase")
}
