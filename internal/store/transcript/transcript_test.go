package transcript

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"komrad/internal/dbtest"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAppendAndGet covers what a conversation reads back as, including before anything has
// been synced for the agent at all.
func TestAppendAndGet(t *testing.T) {
	tests := []struct {
		name  string
		turns [][]Turn
		want  string
	}{
		{
			name: "an unsynced agent reads as no conversation at all",
		},
		{
			name:  "one turn reads back as itself",
			turns: [][]Turn{{{Role: "user", Content: "hello world"}}},
			want:  "hello world",
		},
		{
			name: "a later append follows the turns already stored",
			turns: [][]Turn{
				{{Role: "user", Content: "hello world"}},
				{{Role: "assistant", Content: "hello yourself"}},
			},
			want: "hello world\n\nhello yourself",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			id := seedAgent(t, st, "agent")

			for _, turns := range tt.turns {
				seedTurns(t, st, id, turns)
			}

			got, err := st.Get(t.Context(), id)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestListLastTurns covers reading the newest turns of one role back with the turns between them.
func TestListLastTurns(t *testing.T) {
	stored := []Turn{
		{
			Role:    "user",
			Content: "alpha question",
		},
		{
			Role:    "assistant",
			Content: "alpha answer",
		},
		{
			Role:    "assistant",
			Content: "alpha afterthought",
		},
		{
			Role:    "user",
			Content: "beta question",
		},
		{
			Role:    "assistant",
			Content: "beta answer",
		},
		{
			Role:    "user",
			Content: "gamma question",
		},
	}

	tests := []struct {
		name  string
		role  string
		limit int32
		want  []StoredTurn
	}{
		{
			name:  "the last assistant turn is the newest one alone, whoever spoke after it",
			role:  "assistant",
			limit: 1,
			want: []StoredTurn{
				{
					Seq:     4,
					Role:    "assistant",
					Content: "beta answer",
				},
			},
		},
		{
			name:  "several come back oldest first with the prompts between them and none before",
			role:  "assistant",
			limit: 2,
			want: []StoredTurn{
				{
					Seq:     2,
					Role:    "assistant",
					Content: "alpha afterthought",
				},
				{
					Seq:     3,
					Role:    "user",
					Content: "beta question",
				},
				{
					Seq:     4,
					Role:    "assistant",
					Content: "beta answer",
				},
			},
		},
		{
			name:  "a limit past what is stored runs from the first turn in that role",
			role:  "assistant",
			limit: 10,
			want: []StoredTurn{
				{
					Seq:     1,
					Role:    "assistant",
					Content: "alpha answer",
				},
				{
					Seq:     2,
					Role:    "assistant",
					Content: "alpha afterthought",
				},
				{
					Seq:     3,
					Role:    "user",
					Content: "beta question",
				},
				{
					Seq:     4,
					Role:    "assistant",
					Content: "beta answer",
				},
			},
		},
		{
			name:  "a role nobody spoke in has no turns",
			role:  "system",
			limit: 1,
			want:  []StoredTurn{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			id := seedAgent(t, st, "agent")
			seedTurns(t, st, id, stored)

			got, err := st.ListLastTurns(t.Context(), ListLastTurnsParams{
				AgentID: id,
				Role:    tt.role,
				Limit:   tt.limit,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestListLastTurnsOfUnknownID pins that an unused id and a string that is not an id both read
// as no turns rather than an error.
func TestListLastTurnsOfUnknownID(t *testing.T) {
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

			got, err := st.ListLastTurns(t.Context(), ListLastTurnsParams{
				AgentID: tt.id,
				Role:    "assistant",
				Limit:   1,
			})
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	}
}

// TestGetOfUnknownID pins that an unused id and a string that is not an id both read as the
// zero transcript rather than an error.
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
			assert.Empty(t, got)
		})
	}
}

// TestSearch pins that a search spans archived agents and that each hit says whether the
// agent behind it is still listed.
func TestSearch(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		reindexTo    string
		archive      bool
		other        string
		query        string
		wantHits     int
		wantNear     int
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
			wantNear: 1,
		},
		{
			name:     "a bare exclusion matches nothing rather than everything",
			content:  "alpha beta gamma",
			other:    "delta epsilon",
			query:    "-zeta",
			wantHits: 0,
		},
		{
			name:     "a query of nothing but a stopword is a word like any other",
			content:  "the reconciler notes",
			other:    "zeta alone",
			query:    "the",
			wantHits: 1,
		},
		{
			name:     "an exclusion beside a stopword still excludes",
			content:  "the reconciler notes",
			other:    "the zeta notes",
			query:    "the -zeta",
			wantHits: 1,
		},
		{
			name:     "an excluded stopword still excludes",
			content:  "reconciler notes",
			other:    "the reconciler notes",
			query:    "reconciler -the",
			wantHits: 1,
		},
		{
			name:     "an excluded phrase excludes only the phrase",
			content:  "pane is not done yet",
			other:    "pane is not yet done",
			query:    `pane -"not yet"`,
			wantHits: 1,
		},
		{
			name:     "a quoted phrase finds its stems too, the form typed first",
			content:  "derive marker",
			other:    "derives markers",
			query:    `"derive marker"`,
			wantHits: 2,
		},
		{
			name:     "an exclusion does not leak into the substring fallback",
			content:  "the pgxpool wrapper",
			other:    "the pgxpool sqlite wrapper",
			query:    "pgxp -sqlite",
			wantHits: 1,
			wantNear: 1,
		},
		{
			name:     "quotes do not leak into the substring fallback",
			content:  "the pgxpoolwrapper thing",
			query:    `"pgxpoolwrap"`,
			wantHits: 1,
			wantNear: 1,
		},
		{
			name:     "a flag name is a search rather than an exclusion",
			content:  "the config file lives at ~/.config/komrad/config.toml",
			query:    "--config",
			wantHits: 1,
		},
		{
			name:     "an exclusion in another tense still excludes",
			content:  "the pgxpool notes",
			other:    "the pgxpool wrapper",
			query:    "pgxpool -wrappers",
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
			name:     "a NUL byte is dropped rather than failing the search",
			content:  "findable text",
			query:    "find\x00able",
			wantHits: 1,
		},
		{
			name:     "a query under three characters runs no substring fallback",
			content:  "print the prefix",
			query:    "pr",
			wantHits: 0,
		},
		{
			name:     "a phrase of stop words is found",
			content:  "do it",
			other:    "it is done",
			query:    "do it",
			wantHits: 1,
		},
		{
			name:     "a stop word beside punctuation is still a word to find",
			content:  "Hey. Ready. What need?",
			other:    "nothing needed",
			query:    "What need?",
			wantHits: 1,
		},
		{
			name:     "two stop words both have to appear",
			content:  "the pane is not yet done",
			other:    "yet again the pane",
			query:    "not yet",
			wantHits: 1,
		},
		{
			name:     "a quoted phrase in the wrong order is not found",
			content:  "derives markers from the pane",
			query:    `"markers derives"`,
			wantHits: 0,
		},
		{
			name:     "a phrase of punctuation beside an exclusion matches nothing rather than everything",
			content:  "alpha beta gamma",
			other:    "delta epsilon",
			query:    `"..." -zeta`,
			wantHits: 0,
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
				require.NoError(t, st.DeleteSessionTurns(ctx, openSession(t, st, id).ID))
				seedTranscript(t, st, id, tt.reindexTo)
			}
			if tt.archive {
				archived, findErr := st.agents.GetInWorkflow(ctx, st.workflow, "one")
				require.NoError(t, findErr)
				require.NotNil(t, archived)
				_, archiveErr := st.agents.Archive(ctx, []string{archived.ID})
				require.NoError(t, archiveErr)
			}

			got, err := st.Search(ctx, SearchParams{Query: tt.query, Limit: 10})
			require.NoError(t, err)

			hits := allHits(got)
			require.Len(t, hits, tt.wantHits)
			assert.Len(t, got.Near, tt.wantNear, "which section the hit lands in")

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

// TestSearchReturnsEveryMatchingTurn pins that a subject which came up twice is two hits,
// each pointing at the turn it was said in.
func TestSearchReturnsEveryMatchingTurn(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	turns := []Turn{
		{Role: "user", Content: "how does the reconciler work"},
		{Role: "assistant", Content: "it derives state from the pane"},
		{Role: "user", Content: "and where do the reconciler markers come in"},
	}

	id := seedAgent(t, st, "one")
	seedTurns(t, st, id, turns)

	content, err := st.Get(ctx, id)
	require.NoError(t, err)

	got, err := st.Search(ctx, SearchParams{Query: "reconciler", Limit: 10})
	require.NoError(t, err)

	hits := got.Matches
	require.Len(t, hits, 2, "the turns that mention it, not the conversation once")

	for _, hit := range hits {
		assert.Equal(t, id, hit.AgentID)
		assert.Contains(t, hit.Snippet, SnippetOpen, "the match is marked in the snippet")
		assert.Contains(t, content[hit.ContentOffset:], "reconciler",
			"the offset lands on the turn that matched")
		assert.Contains(t, content[hit.ContentOffset:hit.ContentOffset+hit.ContentLength], "reconciler",
			"the length holds the whole turn the words are looked for in")
	}
	assert.NotEqual(t, hits[0].Seq, hits[1].Seq, "each hit names a different turn")
}

// TestSearchScopedToOneAgent covers the filter a caller narrows a search with, across a
// cursor as well as on a first page.
func TestSearchScopedToOneAgent(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	mine, theirs := seedAgent(t, st, "mine"), seedAgent(t, st, "theirs")

	turns := []Turn{
		{Role: "user", Content: "how does the reconciler work"},
		{Role: "assistant", Content: "the reconciler reads the pane"},
	}
	seedTurns(t, st, mine, turns)
	seedTranscript(t, st, theirs, "the reconciler is described here too")

	unscoped, err := st.Search(ctx, SearchParams{Query: "reconciler", Limit: 10})
	require.NoError(t, err)
	require.Len(t, uniqueStrings(agentIDs(allHits(unscoped))), 2, "both agents said it")

	var (
		ids    []string
		cursor *Cursor
	)
	for range 3 {
		page, err := st.Search(ctx, SearchParams{
			Query:   "reconciler",
			Limit:   1,
			Cursor:  cursor,
			AgentID: &mine,
		})
		require.NoError(t, err)

		hits := allHits(page)
		if len(hits) == 0 {
			break
		}

		ids = append(ids, agentIDs(hits)...)
		cursor = page.Next
	}

	assert.Equal(t, []string{mine, mine}, ids, "both of one agent's turns, and neither of the other's")

	nobody := "not-an-id"
	none, err := st.Search(ctx, SearchParams{
		Query:   "reconciler",
		Limit:   10,
		AgentID: &nobody,
	})
	require.NoError(t, err, "a string that is not an id is a miss rather than a failure")
	assert.Empty(t, allHits(none))
}

// TestSearchPages walks a result set the way the RPC does, covering both the ordering the
// cursor depends on and the paging built on top of it.
func TestSearchPages(t *testing.T) {
	type seed struct {
		name    string
		content string
	}

	tests := []struct {
		name      string
		seeds     []seed
		query     string
		limit     int64
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
			name: "a short turn saying the word outranks a long one saying it many times",
			seeds: []seed{
				{name: "summary", content: strings.Repeat("this session needs the summary, which needs the context, ", 40)},
				{name: "short", content: "Hey. Ready. What need?"},
			},
			query:     "need",
			limit:     10,
			wantNames: []string{"short", "summary"},
			wantCount: 2,
		},
		{
			name: "a turn saying the words as typed outranks a shorter one saying one of them",
			seeds: []seed{
				{name: "bare", content: "not done, yet here"},
				{name: "phrase", content: "the fixture is not yet done, and the goldens are not yet either"},
			},
			query:     "not yet",
			limit:     10,
			wantNames: []string{"phrase", "bare"},
			wantCount: 2,
		},
		{
			name: "punctuation in the query does not stop the phrase being found",
			seeds: []seed{
				{name: "apart", content: "what do we need"},
				{name: "phrase", content: "Hey. Ready. What need?"},
			},
			query:     "What need?",
			limit:     10,
			wantNames: []string{"phrase", "apart"},
			wantCount: 2,
		},
		{
			name: "a word match is listed before a near one, since the sections are in that order",
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
			name: "a cursor walks a set that mixes word and substring hits",
			seeds: []seed{
				{name: "a", content: "shared term here"},
				{name: "b", content: "sharedterm here"},
				{name: "c", content: "shared term here"},
			},
			query:     "shared",
			limit:     1,
			wantCount: 3,
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
				page, err := st.Search(ctx, SearchParams{
					Query:  tt.query,
					Limit:  tt.limit,
					Cursor: cursor,
				})
				require.NoError(t, err)

				hits := allHits(page)
				if len(hits) == 0 {
					break
				}

				for _, hit := range hits {
					names = append(names, hit.Name)
					ids = append(ids, hit.AgentID)
				}

				cursor = page.Next
				if cursor == nil {
					break
				}
			}

			assert.Len(t, names, tt.wantCount)
			assert.Len(t, uniqueStrings(ids), tt.wantCount, "each page returns hits the previous pages did not")

			if tt.wantNames != nil {
				assert.Equal(t, tt.wantNames, names)
			}
		})
	}
}

// TestCursorValidate covers the tokens a search refuses, which a cursor decoding into a position
// the query cannot use would otherwise be answered with an empty page for.
func TestCursorValidate(t *testing.T) {
	valid := Position{
		Key:     0.5,
		AgentID: "00000000-0000-7000-8000-000000000000",
		Seq:     1,
	}

	tests := []struct {
		name    string
		cursor  Cursor
		wantErr bool
	}{
		{
			name:   "a position in one section is enough",
			cursor: Cursor{Sort: SortBest, Matches: &valid},
		},
		{
			name:   "a position in each",
			cursor: Cursor{Sort: SortNewest, Matches: &valid, Near: &valid},
		},
		{
			name:    "a cursor naming no sort was never handed out",
			cursor:  Cursor{Matches: &valid},
			wantErr: true,
		},
		{
			name:    "a cursor locating neither section was never handed out",
			cursor:  Cursor{},
			wantErr: true,
		},
		{
			name:    "a position holding something that is not an agent id",
			cursor:  Cursor{Sort: SortBest, Near: &Position{AgentID: "not-an-id"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cursor.Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestSearchNearWaitsForTheWordMatches pins that the substring fallback stays out of the way
// while there is a page of turns saying the word, and comes back once those have run out.
func TestSearchNearWaitsForTheWordMatches(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	glued := seedAgent(t, st, "glued")
	seedTranscript(t, st, glued, "it lives in /state/komrad/worktree/01a0 today")

	for _, name := range []string{"one", "two", "three", "four"} {
		seedTranscript(t, st, seedAgent(t, st, name), "the worktree is checked out for "+name)
	}

	first, err := st.Search(ctx, SearchParams{Query: "worktree", Limit: 2})
	require.NoError(t, err)
	assert.Len(t, first.Matches, 2, "one page of the turns that say the word")
	assert.Empty(t, first.Near, "and nothing from the substring fallback while that page is full")
	require.NotNil(t, first.Next)

	var near []SearchHit
	cursor := first.Next
	for range 4 {
		page, err := st.Search(ctx, SearchParams{
			Query:  "worktree",
			Limit:  2,
			Cursor: cursor,
		})
		require.NoError(t, err)

		near = append(near, page.Near...)
		cursor = page.Next
		if cursor == nil {
			break
		}
	}

	require.Len(t, near, 1, "the turn that only carries the letters comes once the word matches have run out")
	assert.Equal(t, glued, near[0].AgentID)
}

// TestSearchScoresNearMatches pins that a near match is scored on its own terms rather than against
// the word matches.
func TestSearchScoresNearMatches(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	id := seedAgent(t, st, "partial")
	seedTranscript(t, st, id, "the pgxpool wrapper")

	got, err := st.Search(ctx, SearchParams{Query: "pgxp", Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, got.Matches, "no turn says the word")
	require.Len(t, got.Near, 1)
	assert.Positive(t, got.Near[0].Rank, "a near match carries a score of its own")
}

// TestMarkMatches covers the marking a substring-only hit gets on this side,
// where the offsets of the match have to be offsets into the snippet the caller
// is handed back.
func TestMarkMatches(t *testing.T) {
	tests := []struct {
		name    string
		snippet string
		terms   []string
		want    string
	}{
		{
			name:    "an occurrence is wrapped",
			snippet: "talking about postgres today",
			terms:   []string{"postgres"},
			want:    "talking about " + SnippetOpen + "postgres" + SnippetClose + " today",
		},
		{
			name:    "every occurrence is wrapped",
			snippet: "postgres and postgres",
			terms:   []string{"postgres"},
			want:    SnippetOpen + "postgres" + SnippetClose + " and " + SnippetOpen + "postgres" + SnippetClose,
		},
		{
			name:    "a match of another case keeps the casing the snippet had",
			snippet: "talking about Postgres today",
			terms:   []string{"postgres"},
			want:    "talking about " + SnippetOpen + "Postgres" + SnippetClose + " today",
		},
		{
			name:    "a window cut mid-match is left unmarked",
			snippet: "talking about postg",
			terms:   []string{"postgres"},
			want:    "talking about postg",
		},
		{
			name:    "a query that folds to fewer bytes marks only the bytes it matched",
			snippet: "ok",
			terms:   []string{"K"},
			want:    "o" + SnippetOpen + "k" + SnippetClose,
		},
		{
			name:    "a query that folds to more bytes marks only the bytes it matched",
			snippet: "bitİ",
			terms:   []string{"tİ"},
			want:    "bi" + SnippetOpen + "tİ" + SnippetClose,
		},
		{
			name:    "several terms are each wrapped where they fall",
			snippet: "the pgxpool tuning notes",
			terms:   []string{"pool", "tuning"},
			want: "the pgx" + SnippetOpen + "pool" + SnippetClose + " " +
				SnippetOpen + "tuning" + SnippetClose + " notes",
		},
		{
			name:    "overlapping terms are marked once, the longer of the two",
			snippet: "the pgxpool wrapper",
			terms:   []string{"pgx", "pgxpool"},
			want:    "the " + SnippetOpen + "pgxpool" + SnippetClose + " wrapper",
		},
		{
			name:    "a snippet with no occurrence is returned as it stands",
			snippet: "nothing to see",
			terms:   []string{"postgres"},
			want:    "nothing to see",
		},
		{
			name:    "an empty query marks nothing",
			snippet: "nothing to see",
			terms:   nil,
			want:    "nothing to see",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, markMatches(tt.snippet, tt.terms))
		})
	}
}

// TestSplitTerms covers the words pulled out of a query for the substring fallback, which
// matches text as it was typed and so has to be handed the terms rather than the operators.
func TestSplitTerms(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		wantWanted   []string
		wantExcluded []string
	}{
		{
			name:       "plain words are all wanted",
			query:      "reconciler markers",
			wantWanted: []string{"reconciler", "markers"},
		},
		{
			name:         "a minus marks the word behind it as excluded",
			query:        "postgres -sqlite",
			wantWanted:   []string{"postgres"},
			wantExcluded: []string{"sqlite"},
		},
		{
			name:       "quotes are stripped, and a phrase stays one term",
			query:      `"derives markers" pane`,
			wantWanted: []string{"derives markers", "pane"},
		},
		{
			name:       "a quote closed mid-word keeps the whole word one term",
			query:      `"pgx"pool`,
			wantWanted: []string{"pgxpool"},
		},
		{
			name:         "an excluded phrase keeps its words together",
			query:        `pane -"derives markers"`,
			wantWanted:   []string{"pane"},
			wantExcluded: []string{"derives markers"},
		},
		{
			name:       "or is an ordinary word, since every word typed has to appear",
			query:      "pane or marker",
			wantWanted: []string{"pane", "or", "marker"},
		},
		{
			name:         "a query of nothing but an exclusion asks for no words at all",
			query:        "-zeta",
			wantExcluded: []string{"zeta"},
		},
		{
			name:  "blank space is no query at all",
			query: "   ",
		},
		{
			name:  "a bare minus asks for nothing",
			query: "-",
		},
		{
			name:       "a minus inside a word is part of it",
			query:      "well-known",
			wantWanted: []string{"well-known"},
		},
		{
			name:       "only a single minus excludes, so a flag name is a word",
			query:      "--config",
			wantWanted: []string{"--config"},
		},
		{
			name:       "a quoted minus is part of the word rather than an exclusion",
			query:      `"-config"`,
			wantWanted: []string{"-config"},
		},
		{
			name:       "a minus on its own is not an exclusion of the next word",
			query:      "- config",
			wantWanted: []string{"config"},
		},
		{
			name:       "a trailing minus asks for nothing",
			query:      "config -",
			wantWanted: []string{"config"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitTerms(tt.query)
			assert.Equal(t, tt.wantWanted, got.wanted)
			assert.Equal(t, tt.wantExcluded, got.excluded)
		})
	}
}

// TestQuoteTerms covers how the words a person typed are handed to the database.
func TestQuoteTerms(t *testing.T) {
	tests := []struct {
		name   string
		terms  []string
		prefix string
		want   string
	}{
		{
			name: "no terms is no query",
		},
		{
			name:  "every term is quoted, so or is a word and a phrase stays together",
			terms: []string{"pane", "or", "derives markers"},
			want:  `"pane" "or" "derives markers"`,
		},
		{
			name:   "the prefix goes before each quote",
			terms:  []string{"sqlite", "not yet"},
			prefix: "-",
			want:   `-"sqlite" -"not yet"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, quoteTerms(tt.terms, tt.prefix))
		})
	}
}

// TestPhrasePattern covers the words a query is tested whole for.
func TestPhrasePattern(t *testing.T) {
	tests := []struct {
		name  string
		terms []string
		want  *string
	}{
		{
			name:  "no term at all",
			terms: nil,
			want:  nil,
		},
		{
			name:  "a term holding no word",
			terms: []string{"--", "?"},
			want:  nil,
		},
		{
			name:  "one word",
			terms: []string{"need"},
			want:  xptr.Ptr(`\mneed\M`),
		},
		{
			name:  "punctuation around the words is not part of them",
			terms: []string{"What", "need?"},
			want:  xptr.Ptr(`\mWhat[^[:alnum:]]+need\M`),
		},
		{
			name:  "a quoted phrase and a word run together in order",
			terms: []string{"not yet", "done"},
			want:  xptr.Ptr(`\mnot[^[:alnum:]]+yet[^[:alnum:]]+done\M`),
		},
		{
			name:  "a flag name is the word it looks like",
			terms: []string{"--config"},
			want:  xptr.Ptr(`\mconfig\M`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, phrasePattern(tt.terms))
		})
	}
}

// TestHasNearTerms covers which queries the substring fallback is run for.
func TestHasNearTerms(t *testing.T) {
	tests := []struct {
		name  string
		terms []string
		want  bool
	}{
		{
			name:  "a two-letter term",
			terms: []string{"pr"},
			want:  false,
		},
		{
			name:  "a short term beside a long one",
			terms: []string{"pgxpool", "it"},
			want:  false,
		},
		{
			name:  "three characters of any kind",
			terms: []string{"the"},
			want:  true,
		},
		{
			name:  "runes rather than bytes",
			terms: []string{"héé"},
			want:  true,
		},
		{
			name:  "no term at all",
			terms: nil,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasNearTerms(tt.terms))
		})
	}
}

// TestLikePattern pins that a query is escaped before it reaches LIKE, so a wildcard typed by
// a searcher cannot widen the search.
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

type fixture struct {
	*TranscriptStore
	agents   *agentstore.AgentStore
	db       store.DBTX
	workflow string
}

func open(t *testing.T) *fixture {
	t.Helper()
	p, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	workflows := workflowstore.NewWorkflowStore(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)

	return &fixture{
		TranscriptStore: NewTranscriptStore(p.GetDB()),
		agents:          agentstore.NewAgentStore(p.GetDB()),
		db:              p.GetDB(),
		workflow:        created.ID,
	}
}

func seedAgent(t *testing.T, st *fixture, name string) string {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, st.agents.Create(ctx, agentstore.Agent{
		Name:       name,
		State:      agentstore.StateIdle,
		WorkflowID: st.workflow,
	}))
	got, err := st.agents.GetInWorkflow(ctx, st.workflow, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}

func seedTranscript(t *testing.T, st *fixture, id, content string) {
	t.Helper()
	seedTurns(t, st, id, []Turn{{Role: "user", Content: content}})
}

func seedTurns(t *testing.T, st *fixture, id string, turns []Turn) {
	t.Helper()
	require.NoError(t, st.AppendTurns(t.Context(), AppendTurnsParams{
		AgentID:   id,
		SessionID: openSession(t, st, id).ID,
		Turns:     turns,
	}))
}

func openSession(t *testing.T, st *fixture, id string) Session {
	t.Helper()
	session, err := st.OpenSession(t.Context(), OpenSessionParams{
		AgentID:   id,
		SessionID: "session-" + id,
	})
	require.NoError(t, err)

	return session
}

// allHits flattens both sections in the order the panel shows them, for the tests that care
// how many turns matched rather than which way each one did.
func allHits(results Results) []SearchHit {
	return append(append([]SearchHit{}, results.Matches...), results.Near...)
}

func agentIDs(hits []SearchHit) []string {
	ids := xslice.Map(hits, func(hit SearchHit) string {
		return hit.AgentID
	})

	return ids
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

// TestAppendTurnsPlacesEachTurn covers the offset stored against a turn.
func TestAppendTurnsPlacesEachTurn(t *testing.T) {
	tests := []struct {
		name   string
		passes [][]Turn
		want   string
	}{
		{
			name:   "nothing to append leaves the conversation empty",
			passes: [][]Turn{{}},
		},
		{
			name:   "the first turns start the conversation",
			passes: [][]Turn{{{Role: "user", Content: "alpha question"}, {Role: "assistant", Content: "beta answer"}}},
			want:   "alpha question\n\nbeta answer",
		},
		{
			name: "later turns are placed after what is already stored",
			passes: [][]Turn{
				{{Role: "user", Content: "alpha earlier"}},
				{{Role: "user", Content: "beta later"}},
			},
			want: "alpha earlier\n\nbeta later",
		},
		{
			name:   "a turn saying nothing still takes its separator",
			passes: [][]Turn{{{Role: "user", Content: ""}, {Role: "assistant", Content: "alpha answer"}}},
			want:   "\n\nalpha answer",
		},
		{
			name: "a turn holding multi-byte runes is measured in bytes",
			passes: [][]Turn{
				{{Role: "user", Content: "alpha héllo wörld"}},
				{{Role: "user", Content: "beta after"}},
			},
			want: "alpha héllo wörld\n\nbeta after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			id := seedAgent(t, st, "agent")

			for _, turns := range tt.passes {
				seedTurns(t, st, id, turns)
			}

			content, err := st.Get(t.Context(), id)
			require.NoError(t, err)
			require.Equal(t, tt.want, content)

			for _, turns := range tt.passes {
				for _, turn := range turns {
					words := strings.Fields(turn.Content)
					if len(words) == 0 {
						continue
					}

					hit := onlyHit(t, st, id, words[0])
					assert.Equal(t, turn.Content, content[hit.ContentOffset:hit.ContentOffset+hit.ContentLength],
						"the offset and length cut the turn that matched out of the conversation")
				}
			}
		})
	}
}

func onlyHit(t *testing.T, st *fixture, id, query string) SearchHit {
	t.Helper()
	got, err := st.Search(t.Context(), SearchParams{
		Query:   query,
		AgentID: &id,
		Limit:   10,
	})
	require.NoError(t, err)

	hits := allHits(got)
	require.Len(t, hits, 1)

	return hits[0]
}

type narrowed struct {
	*fixture
	start time.Time
}

func seedNarrowed(t *testing.T) narrowed {
	t.Helper()
	st := open(t)
	ctx := t.Context()
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	web, err := workflowstore.NewWorkflowStore(st.db).Create(ctx, workflowstore.Config{Name: "komrad-web", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)

	alpha := seedAgentAs(t, st, agentstore.Agent{Name: "alpha", WorkflowID: st.workflow})
	beta := seedAgentAs(t, st, agentstore.Agent{Name: "beta", Role: agentstore.RoleReviewer, WorkflowID: st.workflow})
	gamma := seedAgentAs(t, st, agentstore.Agent{Name: "gamma", Role: agentstore.RoleOrchestrator, WorkflowID: web.ID})
	delta := seedAgentAs(t, st, agentstore.Agent{Name: "delta", WorkflowID: st.workflow})

	seedTurns(t, st, alpha, []Turn{
		{Role: "user", Content: "a reconciler question", SpokenAt: start},
		{Role: "assistant", Content: "a reconciler answer", SpokenAt: start.Add(time.Hour)},
	})
	seedTurns(t, st, beta, []Turn{{Role: "user", Content: "a reconciler review", SpokenAt: start.Add(48 * time.Hour)}})
	seedTurns(t, st, gamma, []Turn{{Role: "assistant", Content: "a reconciler plan", SpokenAt: start.Add(72 * time.Hour)}})
	seedTurns(t, st, delta, []Turn{
		{Role: "user", Content: "a reconciler from before times were kept"},
		{Role: "user", Content: "a reconciler also from before", SpokenAt: time.Time{}},
	})

	_, err = st.agents.Archive(ctx, []string{beta})
	require.NoError(t, err)

	return narrowed{fixture: st, start: start}
}

func seedAgentAs(t *testing.T, st *fixture, agent agentstore.Agent) string {
	t.Helper()
	agent.State = agentstore.StateIdle
	require.NoError(t, st.agents.Create(t.Context(), agent))
	got, err := st.agents.GetInWorkflow(t.Context(), agent.WorkflowID, agent.Name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}

func hitNames(hits []SearchHit) []string {
	names := xslice.Map(hits, func(hit SearchHit) string {
		return fmt.Sprintf("%s:%d", hit.Name, hit.Seq)
	})

	return names
}

// TestSearchFilter covers each condition a filter sets, in both sections.
func TestSearchFilter(t *testing.T) {
	st := seedNarrowed(t)
	dayAfter := st.start.Add(24 * time.Hour)

	tests := []struct {
		name   string
		query  string
		filter Filter
		want   []string
	}{
		{
			name:   "what arrived as a prompt",
			filter: Filter{Speaker: xptr.Ptr("user")},
			want:   []string{"alpha:0", "beta:0", "delta:0", "delta:1"},
		},
		{
			name:   "what the agent wrote",
			filter: Filter{Speaker: xptr.Ptr("assistant")},
			want:   []string{"alpha:1", "gamma:0"},
		},
		{
			name:   "agents whose name holds the text, in any case",
			filter: Filter{AgentName: xptr.Ptr("LPH")},
			want:   []string{"alpha:0", "alpha:1"},
		},
		{
			name:   "agents in a workflow whose name holds the text",
			filter: Filter{WorkflowName: xptr.Ptr("web")},
			want:   []string{"gamma:0"},
		},
		{
			name:   "one kind of agent",
			filter: Filter{Kinds: []string{"reviewer"}},
			want:   []string{"beta:0"},
		},
		{
			name:   "kinds left out",
			filter: Filter{ExcludedKinds: []string{"reviewer", "orchestrator"}},
			want:   []string{"alpha:0", "alpha:1", "delta:0", "delta:1"},
		},
		{
			name:   "archived agents",
			filter: Filter{Archived: xptr.Ptr(true)},
			want:   []string{"beta:0"},
		},
		{
			name:   "live agents",
			filter: Filter{Archived: xptr.Ptr(false)},
			want:   []string{"alpha:0", "alpha:1", "gamma:0", "delta:0", "delta:1"},
		},
		{
			name:   "said since a time, leaving out turns with no time",
			filter: Filter{Since: &dayAfter},
			want:   []string{"beta:0", "gamma:0"},
		},
		{
			name:   "said before a time",
			filter: Filter{Until: &dayAfter},
			want:   []string{"alpha:0", "alpha:1"},
		},
		{
			name:   "the near section applies the filter too",
			query:  "reconcil",
			filter: Filter{Speaker: xptr.Ptr("assistant")},
			want:   []string{"alpha:1", "gamma:0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := tt.query
			if query == "" {
				query = "reconciler"
			}

			got, err := st.Search(t.Context(), SearchParams{Query: query, Limit: 10, Filter: tt.filter})
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, hitNames(allHits(got)))
		})
	}
}

// TestSearchSort covers the order each sort walks the hits in, page by page, with turns that
// have no time last.
func TestSearchSort(t *testing.T) {
	st := seedNarrowed(t)

	tests := []struct {
		name  string
		query string
		sort  Sort
		limit int64
		want  []string
	}{
		{
			name: "newest first",
			sort: SortNewest,
			want: []string{"gamma:0", "beta:0", "alpha:1", "alpha:0", "delta:0", "delta:1"},
		},
		{
			name: "oldest first",
			sort: SortOldest,
			want: []string{"alpha:0", "alpha:1", "beta:0", "gamma:0", "delta:0", "delta:1"},
		},
		{
			name:  "a page ending inside the turns with no time carries on without repeating one",
			sort:  SortNewest,
			limit: 5,
			want:  []string{"gamma:0", "beta:0", "alpha:1", "alpha:0", "delta:0", "delta:1"},
		},
		{
			name:  "the near section in time order",
			query: "reconcil",
			sort:  SortNewest,
			want:  []string{"gamma:0", "beta:0", "alpha:1", "alpha:0", "delta:0", "delta:1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := tt.query
			if query == "" {
				query = "reconciler"
			}

			limit := tt.limit
			if limit == 0 {
				limit = 2
			}

			var (
				names  []string
				cursor *Cursor
			)
			for range 10 {
				page, err := st.Search(t.Context(), SearchParams{Query: query, Limit: limit, Sort: tt.sort, Cursor: cursor})
				require.NoError(t, err)

				names = append(names, hitNames(allHits(page))...)
				if page.Next == nil {
					break
				}
				cursor = page.Next
			}

			assert.Equal(t, tt.want, names)
		})
	}
}

// TestSearchWithOnlyAFilter covers a query naming no word, which lists the turns passing its
// filter newest first, from the start of each turn.
func TestSearchWithOnlyAFilter(t *testing.T) {
	st := seedNarrowed(t)
	user := Filter{Speaker: xptr.Ptr("user")}

	tests := []struct {
		name   string
		query  string
		filter Filter
		want   []string
	}{
		{
			name: "no filter and no word is nothing",
			want: []string{},
		},
		{
			name:   "every turn passing the filter",
			filter: user,
			want:   []string{"beta:0", "alpha:0", "delta:0", "delta:1"},
		},
		{
			name:   "less the turns saying a word left out",
			query:  "-question",
			filter: user,
			want:   []string{"beta:0", "delta:0", "delta:1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.Search(t.Context(), SearchParams{Query: tt.query, Limit: 10, Filter: tt.filter})
			require.NoError(t, err)
			assert.Equal(t, tt.want, hitNames(allHits(got)))
			assert.Empty(t, got.Near)
		})
	}

	got, err := st.Search(t.Context(), SearchParams{Limit: 1, Filter: Filter{AgentName: xptr.Ptr("alpha")}})
	require.NoError(t, err)
	require.Len(t, got.Matches, 1)
	assert.Equal(t, "a reconciler answer", got.Matches[0].Snippet)
	assert.Equal(t, st.start.Add(time.Hour), got.Matches[0].SpokenAt.UTC())
}

// TestSearchRefusesACursorFromAnotherSort pins that a page token only continues the order it
// was handed out under.
func TestSearchRefusesACursorFromAnotherSort(t *testing.T) {
	st := seedNarrowed(t)

	first, err := st.Search(t.Context(), SearchParams{Query: "reconciler", Limit: 1, Sort: SortNewest})
	require.NoError(t, err)
	require.NotNil(t, first.Next)

	_, err = st.Search(t.Context(), SearchParams{Query: "reconciler", Limit: 1, Cursor: first.Next})
	require.ErrorIs(t, err, ErrSortChanged)
}

// TestAppendTurnsStoresNoTimeForATurnWithout pins that a turn read without a time is stored
// with none, rather than as the zero time.
func TestAppendTurnsStoresNoTimeForATurnWithout(t *testing.T) {
	st := open(t)
	id := seedAgent(t, st, "untimed")
	seedTurns(t, st, id, []Turn{{Role: "user", Content: "a reconciler with no time"}})

	assert.Nil(t, onlyHit(t, st, id, "reconciler").SpokenAt)
}
