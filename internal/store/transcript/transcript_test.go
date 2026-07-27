package transcript

import (
	"testing"

	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"
	agentstore "claude-agents/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpsertAndGet covers what a transcript reads back as, including before
// anything has been synced for the agent at all.
func TestUpsertAndGet(t *testing.T) {
	tests := []struct {
		name string
		// writes are applied in order, so a case can show a later write landing on
		// top of an earlier one rather than beside it.
		writes      []UpsertParams
		wantContent string
		wantOffset  int64
	}{
		{
			name: "an unsynced agent reads as the zero transcript",
		},
		{
			name:        "content and offset round-trip",
			writes:      []UpsertParams{{Content: "hello world", Offset: 11}},
			wantContent: "hello world",
			wantOffset:  11,
		},
		{
			name: "a later write replaces the content in place",
			writes: []UpsertParams{
				{Content: "hello world", Offset: 11},
				{Content: "hello world again", Offset: 17},
			},
			wantContent: "hello world again",
			wantOffset:  17,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()
			id := seedAgent(t, st, "agent")

			for _, write := range tt.writes {
				write.AgentID = id
				require.NoError(t, st.Upsert(ctx, write))
			}

			got, err := st.Get(ctx, id)
			require.NoError(t, err)
			assert.Equal(t, tt.wantContent, got.Content)
			assert.Equal(t, tt.wantOffset, got.SyncedOffset)
		})
	}
}

// TestGetOfUnknownID covers the two ways an id can fail to name an agent: one
// that is well-formed but unused, and one that is not an id at all. Both read as
// the zero transcript rather than an error, so a caller that mistyped an id gets
// an empty result instead of a failure.
func TestGetOfUnknownID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{
			name: "a malformed id is not an agent",
			id:   "missing",
		},
		{
			name: "a well-formed id nothing holds",
			id:   "00000000-0000-7000-8000-000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)

			got, err := st.Get(t.Context(), tt.id)
			require.NoError(t, err)
			assert.Equal(t, Transcript{}, got)
		})
	}
}

// TestSearch covers what a query matches and what a hit carries. Search spans
// archived agents, since finding a conversation is the reason to look for one,
// and each hit says whether the agent behind it is still listed.
func TestSearch(t *testing.T) {
	tests := []struct {
		name    string
		content string
		// reindexTo replaces the content with a second write before the search.
		reindexTo string
		archive   bool
		// other is a second agent's transcript, for the cases that only mean
		// something when there is something to leave out.
		other       string
		query       string
		wantHits    int
		wantDeleted bool
	}{
		{
			name:     "a blank query matches nothing rather than everything",
			content:  "anything at all",
			query:    "   ",
			wantHits: 0,
		},
		{
			name:      "content replaced by a later write is no longer indexed",
			content:   "talking about postgres",
			reindexTo: "now talking about something else",
			query:     "postgres",
			wantHits:  0,
		},
		{
			name:     "a whole word",
			content:  "the quick brown fox discusses postgres indexing",
			query:    "postgres",
			wantHits: 1,
		},
		{
			name:     "a substring, since the index is trigram",
			content:  "the quick brown fox discusses postgres indexing",
			query:    "ostgr",
			wantHits: 1,
		},
		{
			name:        "an archived agent is still findable",
			content:     "an agent that mentions postgres then is archived",
			archive:     true,
			query:       "postgres",
			wantHits:    1,
			wantDeleted: true,
		},
		{
			name:     "an unrelated transcript is left out",
			content:  "the quick brown fox discusses postgres indexing",
			other:    "an unrelated agent about breakfast cereal",
			query:    "postgres",
			wantHits: 1,
		},
		{
			// The behaviour the LIKE escaping exists for: the percent matches itself
			// rather than standing in for any run of text.
			name:     "a wildcard in the query is matched literally",
			content:  "the coverage hit 100% today",
			other:    "the coverage hit 100 of them today",
			query:    "100%",
			wantHits: 1,
		},
		{
			name:     "operator syntax and quotes are matched rather than parsed",
			content:  `the config uses a "flags" field and an OR clause`,
			query:    `"flags" field`,
			wantHits: 1,
		},
		{
			// Postgres refuses a NUL in a text value outright, so it is dropped
			// before the query is sent rather than failing the request.
			name:     "a NUL byte is dropped rather than failing the search",
			content:  "findable text",
			query:    "find\x00able",
			wantHits: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			id := seedAgent(t, st, "one")
			require.NoError(t, st.Upsert(ctx, UpsertParams{AgentID: id, Content: tt.content, Offset: 1}))

			if tt.other != "" {
				second := seedAgent(t, st, "two")
				require.NoError(t, st.Upsert(ctx, UpsertParams{AgentID: second, Content: tt.other, Offset: 1}))
			}
			if tt.reindexTo != "" {
				require.NoError(t, st.Upsert(ctx, UpsertParams{AgentID: id, Content: tt.reindexTo, Offset: 2}))
			}
			if tt.archive {
				require.NoError(t, st.agents.Archive(ctx, "one"))
			}

			hits, err := st.Search(ctx, SearchParams{Query: tt.query, Limit: 10})
			require.NoError(t, err)
			require.Len(t, hits, tt.wantHits)

			if tt.wantHits == 0 {
				return
			}

			assert.Equal(t, id, hits[0].AgentID)
			assert.Equal(t, "one", hits[0].Name, "the hit carries the agent's name")
			assert.Equal(t, tt.wantDeleted, hits[0].Deleted)
			assert.Contains(t, hits[0].Snippet, SnippetOpen, "the match is marked in the snippet")
		})
	}
}

// TestSearchPages walks a result set the way the RPC does, following the cursor
// until it runs dry. That covers both the ordering the cursor depends on and the
// paging built on top of it.
func TestSearchPages(t *testing.T) {
	type seed struct {
		name    string
		content string
	}

	tests := []struct {
		name  string
		seeds []seed
		query string
		limit int64
		// wantNames, when set, is the order the hits must come back in. Cases whose
		// hits all rank equally leave it unset, since the tiebreak is by id.
		wantNames []string
		wantRanks []int32
		wantCount int
	}{
		{
			name: "hits are ordered by how often the query appears",
			seeds: []seed{
				{name: "once", content: "we mentioned pgx here"},
				{name: "thrice", content: "pgx, and pgx again, and pgx once more"},
			},
			query:     "pgx",
			limit:     10,
			wantNames: []string{"thrice", "once"},
			wantRanks: []int32{3, 1},
			wantCount: 2,
		},
		{
			name: "a cursor walks the whole set one hit at a time",
			seeds: []seed{
				{name: "a", content: "shared term here"},
				{name: "b", content: "shared term here"},
				{name: "c", content: "shared term here"},
			},
			query:     "shared",
			limit:     1,
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			for _, s := range tt.seeds {
				id := seedAgent(t, st, s.name)
				require.NoError(t, st.Upsert(ctx, UpsertParams{AgentID: id, Content: s.content, Offset: 1}))
			}

			var (
				names  []string
				ids    []string
				ranks  []int32
				cursor *Cursor
			)
			for range len(tt.seeds) + 1 {
				hits, err := st.Search(ctx, SearchParams{Query: tt.query, Limit: tt.limit, Cursor: cursor})
				require.NoError(t, err)
				if len(hits) == 0 {
					break
				}

				for _, hit := range hits {
					names = append(names, hit.Name)
					ids = append(ids, hit.AgentID)
					ranks = append(ranks, hit.Rank)
				}

				last := hits[len(hits)-1]
				cursor = &Cursor{Rank: last.Rank, AgentID: last.AgentID}
			}

			assert.Len(t, names, tt.wantCount)
			assert.Len(t, uniqueStrings(ids), tt.wantCount, "each page returns hits the previous pages did not")

			if tt.wantNames != nil {
				assert.Equal(t, tt.wantNames, names)
			}
			if tt.wantRanks != nil {
				assert.Equal(t, tt.wantRanks, ranks)
			}
		})
	}
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

// fixture is the transcript store under test plus the agent store the tests seed
// through. A transcript is keyed to an agent, and search joins the agents table
// for the name and archived flag, so a hit needs a real agent behind it. The two
// stay separate stores: this package never writes that table itself.
type fixture struct {
	*Store
	agents *agentstore.Store
}

func open(t *testing.T) *fixture {
	t.Helper()
	p, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return &fixture{Store: New(p.DB()), agents: agentstore.New(p.DB())}
}

// seedAgent inserts an agent and returns its generated id, so a transcript can
// be keyed to a real row the search join can resolve.
func seedAgent(t *testing.T, st *fixture, name string) string {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, st.agents.Upsert(ctx, agentstore.Agent{Name: name, State: agentstore.StateNew}))
	got, err := st.agents.Get(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
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
