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

func TestSearchTranscripts(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	require.NoError(t, st.UpsertTranscript(ctx, "one", "the quick brown fox discusses postgres indexing", 1))
	require.NoError(t, st.UpsertTranscript(ctx, "two", "an unrelated chat about breakfast cereal", 1))

	hits, err := st.SearchTranscripts(ctx, "postgres", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1, "only the matching transcript is returned")
	assert.Equal(t, "one", hits[0].SessionID)
	assert.Contains(t, hits[0].Snippet, SnippetOpen+"postgres"+SnippetClose, "the match is marked in the snippet")

	hits, err = st.SearchTranscripts(ctx, "ostgr", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1, "trigram tokenizer matches on a substring, not just whole words")
	assert.Equal(t, "one", hits[0].SessionID)
}

func TestSearchTranscriptsBlankQuery(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	require.NoError(t, st.UpsertTranscript(ctx, "one", "anything at all", 1))

	hits, err := st.SearchTranscripts(ctx, "   ", 10)
	require.NoError(t, err)
	assert.Empty(t, hits, "a blank query returns nothing rather than every transcript")
}

func TestSearchTranscriptsReindexOnUpsert(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	require.NoError(t, st.UpsertTranscript(ctx, "one", "talking about postgres", 1))
	require.NoError(t, st.UpsertTranscript(ctx, "one", "now talking about sqlite instead", 2))

	hits, err := st.SearchTranscripts(ctx, "postgres", 10)
	require.NoError(t, err)
	assert.Empty(t, hits, "the old content is dropped from the index on re-upsert")

	hits, err = st.SearchTranscripts(ctx, "sqlite", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "one", hits[0].SessionID)
}

func TestFTSPhrase(t *testing.T) {
	assert.Equal(t, `"plain"`, ftsPhrase("plain"))
	assert.Equal(t, `"say ""hi"""`, ftsPhrase(`say "hi"`), "embedded quotes are doubled")
}

func TestSearchTranscriptsQueryWithOperators(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	require.NoError(t, st.UpsertTranscript(ctx, "one", `the config uses a "flags" field and an OR clause`, 1))

	// A query carrying FTS operator syntax and quotes must be matched literally,
	// not parsed, so it neither errors nor mis-matches.
	hits, err := st.SearchTranscripts(ctx, `"flags" field`, 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "one", hits[0].SessionID)
	assert.True(t, strings.Contains(hits[0].Snippet, SnippetOpen), "the literal phrase is found and marked")
}
