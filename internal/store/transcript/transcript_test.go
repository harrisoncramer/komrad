package transcript

import (
	"strings"
	"testing"

	"weave/internal/pgtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

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

// TestGetOfUnknownID covers the two ways an id can fail to name an agent. One
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
		other        string
		query        string
		wantHits     int
		wantArchived bool
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
			name:     "a word the query stems to, rather than the form typed",
			content:  "the reconciler derives its markers",
			query:    "reconciling",
			wantHits: 1,
		},
		{
			name:     "several words, which all have to appear",
			content:  "the reconciler derives its markers",
			other:    "the reconciler runs on a timer",
			query:    "reconciler markers",
			wantHits: 1,
		},
		{
			name:     "a quoted phrase has to appear in that order",
			content:  "derives markers from the pane",
			other:    "markers derives nothing in this order",
			query:    `"derives markers"`,
			wantHits: 1,
		},
		{
			name:     "a word behind a minus is excluded",
			content:  "postgres indexing notes",
			other:    "postgres indexing and sqlite notes",
			query:    "postgres -sqlite",
			wantHits: 1,
		},
		{
			name:     "a partial word, which the trigram fallback covers",
			content:  "the quick brown fox discusses postgres indexing",
			query:    "ostgr",
			wantHits: 1,
		},
		{
			name:         "an archived agent is still findable",
			content:      "an agent that mentions postgres then is archived",
			archive:      true,
			query:        "postgres",
			wantHits:     1,
			wantArchived: true,
		},
		{
			name:     "an unrelated transcript is left out",
			content:  "the quick brown fox discusses postgres indexing",
			other:    "an unrelated agent about breakfast cereal",
			query:    "postgres",
			wantHits: 1,
		},
		{
			// The percent is punctuation to the tokenizer, so both transcripts match
			// on the number alone. What it must not do is act as a wildcard in the
			// fallback and widen the search further, which is what the escaping in
			// likePattern prevents.
			name:     "a wildcard in the query does not widen it",
			content:  "the coverage hit 100% today",
			other:    "the coverage hit 100 of them today",
			query:    "100%",
			wantHits: 2,
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
		{
			// The Kelvin sign lowercases to a plain k, three bytes to one, so
			// Postgres matches it against ordinary text and the hit comes back
			// ranked zero for this side to mark. The match it has to mark is one
			// byte where the query is three.
			name:     "a query that lowercases to fewer bytes than it was typed",
			content:  "ok",
			query:    "K",
			wantHits: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			id := seedAgent(t, st, "one")
			seedTranscript(t, st, id, tt.content)

			if tt.other != "" {
				second := seedAgent(t, st, "two")
				seedTranscript(t, st, second, tt.other)
			}
			if tt.reindexTo != "" {
				require.NoError(t, st.DeleteTurns(ctx, id))
				seedTranscript(t, st, id, tt.reindexTo)
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
			assert.Equal(t, tt.wantArchived, hits[0].Archived)
			assert.Contains(t, hits[0].Snippet, SnippetOpen, "the match is marked in the snippet")
		})
	}
}

// TestSearchReturnsEveryMatchingTurn covers the shape a conversation matches in. A
// subject that came up twice is two hits rather than one, each pointing at the turn
// it was said in.
func TestSearchReturnsEveryMatchingTurn(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	content, turns := layOutTurns([]Turn{
		{Role: "user", Content: "how does the reconciler work"},
		{Role: "assistant", Content: "it derives state from the pane"},
		{Role: "user", Content: "and where do the reconciler markers come in"},
	})

	id := seedAgent(t, st, "one")
	seedTurns(t, st, id, content, turns)

	hits, err := st.Search(ctx, SearchParams{Query: "reconciler", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 2, "the turns that mention it, not the conversation once")

	for _, hit := range hits {
		assert.Equal(t, id, hit.AgentID)
		assert.Contains(t, hit.Snippet, SnippetOpen, "the match is marked in the snippet")
		assert.Contains(t, content[hit.ContentOffset:], "reconciler",
			"the offset lands on the turn that matched")
	}
	assert.NotEqual(t, hits[0].Seq, hits[1].Seq, "each hit names a different turn")
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
		wantCount int
	}{
		{
			name: "a turn the query fits closely outranks one it barely touches",
			seeds: []seed{
				{name: "close", content: "pgx pool tuning"},
				{name: "distant", content: "pgx is mentioned here and pool comes up much later after many other unrelated words"},
			},
			query:     "pgx pool",
			limit:     10,
			wantNames: []string{"close", "distant"},
			wantCount: 2,
		},
		{
			name: "a word match outranks a partial one",
			seeds: []seed{
				{name: "word", content: "we use pgx here"},
				{name: "partial", content: "we use pgxpool here"},
			},
			query:     "pgx",
			limit:     10,
			wantNames: []string{"word", "partial"},
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
				seedTranscript(t, st, seedAgent(t, st, s.name), s.content)
			}

			var (
				names  []string
				ids    []string
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
				}

				last := hits[len(hits)-1]
				cursor = &Cursor{Rank: last.Rank, AgentID: last.AgentID, Seq: last.Seq}
			}

			assert.Len(t, names, tt.wantCount)
			assert.Len(t, uniqueStrings(ids), tt.wantCount, "each page returns hits the previous pages did not")

			if tt.wantNames != nil {
				assert.Equal(t, tt.wantNames, names)
			}
		})
	}
}

// TestMarkMatches covers the marking a substring-only hit gets on this side,
// where the offsets of the match have to be offsets into the snippet the caller
// is handed back.
func TestMarkMatches(t *testing.T) {
	tests := []struct {
		name    string
		snippet string
		query   string
		want    string
	}{
		{
			name:    "an occurrence is wrapped",
			snippet: "talking about postgres today",
			query:   "postgres",
			want:    "talking about " + SnippetOpen + "postgres" + SnippetClose + " today",
		},
		{
			name:    "every occurrence is wrapped",
			snippet: "postgres and postgres",
			query:   "postgres",
			want:    SnippetOpen + "postgres" + SnippetClose + " and " + SnippetOpen + "postgres" + SnippetClose,
		},
		{
			name:    "a match of another case keeps the casing the snippet had",
			snippet: "talking about Postgres today",
			query:   "postgres",
			want:    "talking about " + SnippetOpen + "Postgres" + SnippetClose + " today",
		},
		{
			name:    "a window cut mid-match is left unmarked",
			snippet: "talking about postg",
			query:   "postgres",
			want:    "talking about postg",
		},
		{
			// The Kelvin sign is three bytes and lowercases to a one-byte k, so a
			// match found by folding is shorter than the query that found it.
			// Marking it by the query's own length would wrap the wrong run, and at
			// the end of a window it would slice past the snippet entirely.
			name:    "a query that folds to fewer bytes marks only the bytes it matched",
			snippet: "ok",
			query:   "K",
			want:    "o" + SnippetOpen + "k" + SnippetClose,
		},
		{
			// The other direction. The dotted capital I lowercases to two runes, so
			// the match is longer than the query.
			name:    "a query that folds to more bytes marks only the bytes it matched",
			snippet: "bitİ",
			query:   "tİ",
			want:    "bi" + SnippetOpen + "tİ" + SnippetClose,
		},
		{
			name:    "a snippet with no occurrence is returned as it stands",
			snippet: "nothing to see",
			query:   "postgres",
			want:    "nothing to see",
		},
		{
			name:    "an empty query marks nothing",
			snippet: "nothing to see",
			query:   "",
			want:    "nothing to see",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, markMatches(tt.snippet, tt.query))
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
// through. A transcript is keyed to an agent, and search joins the agents table for
// the name and whether the agent is archived, so a hit needs a real agent behind it.
// The two stay separate stores, so this package never writes that table itself.
type fixture struct {
	*Store
	agents *agentstore.Store
	// workflow holds the agents these tests seed. Every agent belongs to one, and
	// none of these cases are about which.
	workflow string
}

func open(t *testing.T) *fixture {
	t.Helper()
	p, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	workflows := workflowstore.New(p.DB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default"})
	require.NoError(t, err)

	return &fixture{Store: New(p.DB()), agents: agentstore.New(p.DB()), workflow: created.ID}
}

// seedAgent inserts an agent and returns its generated id, so a transcript can
// be keyed to a real row the search join can resolve.
func seedAgent(t *testing.T, st *fixture, name string) string {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, st.agents.Create(ctx, agentstore.Agent{
		Name: name, State: agentstore.StateIdle, WorkflowID: st.workflow,
	}))
	got, err := st.agents.Get(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}

// seedTranscript writes an agent's transcript and indexes it as one turn, which is
// what a sync leaves behind for a conversation that is one turn long.
func seedTranscript(t *testing.T, st *fixture, id, content string) {
	t.Helper()
	seedTurns(t, st, id, content, []Turn{{Role: "user", Content: content}})
}

// layOutTurns lays turns out the way a sync does, blank-line separated, and stamps
// each with where it landed, so a test asserting on offsets does not hand-count
// bytes to get them.
func layOutTurns(turns []Turn) (string, []Turn) {
	var content strings.Builder

	out := make([]Turn, 0, len(turns))
	for _, turn := range turns {
		if content.Len() > 0 {
			content.WriteString("\n\n")
		}
		turn.Offset = int64(content.Len())
		out = append(out, turn)
		content.WriteString(turn.Content)
	}

	return content.String(), out
}

// seedTurns writes an agent's transcript and indexes the turns making it up, for
// the cases that care where inside a conversation a match landed.
func seedTurns(t *testing.T, st *fixture, id, content string, turns []Turn) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, st.Upsert(ctx, UpsertParams{AgentID: id, Content: content, Offset: 1}))
	require.NoError(t, st.AppendTurns(ctx, id, turns))
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
