package transcript

import (
	"strings"
	"testing"

	"weave/internal/dbtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

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
			name:     "a query of nothing but a stopword does not match every turn",
			content:  "the reconciler notes",
			other:    "zeta alone",
			query:    "the",
			wantHits: 1,
			wantNear: 1,
		},
		{
			name:     "an exclusion beside a stopword still excludes",
			content:  "the reconciler notes",
			other:    "the zeta notes",
			query:    "the -zeta",
			wantHits: 1,
			wantNear: 1,
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
			content:  "the config file lives at ~/.config/weave/config.toml",
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
			name:     "a query that lowercases to fewer bytes than it was typed",
			content:  "ok",
			query:    "K",
			wantHits: 1,
			wantNear: 1,
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
		Rank:    0.5,
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
			cursor: Cursor{Matches: &valid},
		},
		{
			name:   "a position in each",
			cursor: Cursor{Matches: &valid, Near: &valid},
		},
		{
			name:    "a cursor locating neither section was never handed out",
			cursor:  Cursor{},
			wantErr: true,
		},
		{
			name:    "a position holding something that is not an agent id",
			cursor:  Cursor{Near: &Position{AgentID: "not-an-id"}},
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

// TestSearchKeepsNearMatchesOnTheirOwnPage pins the point of holding the two apart: a near
// match comes back however many word matches there are, where one shared ranking left it
// behind all of them and off the page the panel asks for.
func TestSearchKeepsNearMatchesOnTheirOwnPage(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	glued := seedAgent(t, st, "glued")
	seedTranscript(t, st, glued, "it lives in /state/weave/worktree/01a0 today")

	for _, name := range []string{"one", "two", "three", "four"} {
		seedTranscript(t, st, seedAgent(t, st, name), "the worktree is checked out for "+name)
	}

	got, err := st.Search(ctx, SearchParams{Query: "worktree", Limit: 2})
	require.NoError(t, err)

	assert.Len(t, got.Matches, 2, "one page of the turns that say the word")
	require.Len(t, got.Near, 1, "and the turn that only carries the letters, not buried behind them")
	assert.Equal(t, glued, got.Near[0].AgentID)
	assert.NotContains(t, agentIDs(got.Matches), glued, "a turn is in one section or the other")
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
		wantPositive []string
		wantNegative []string
	}{
		{
			name:         "plain words are all positive",
			query:        "reconciler markers",
			wantPositive: []string{"reconciler", "markers"},
		},
		{
			name:         "a minus marks the word behind it as excluded",
			query:        "postgres -sqlite",
			wantPositive: []string{"postgres"},
			wantNegative: []string{"sqlite"},
		},
		{
			name:         "quotes are stripped, and a phrase stays one term",
			query:        `"derives markers" pane`,
			wantPositive: []string{"derives markers", "pane"},
		},
		{
			name:         "an excluded phrase keeps its words together",
			query:        `pane -"derives markers"`,
			wantPositive: []string{"pane"},
			wantNegative: []string{"derives markers"},
		},
		{
			name:         "or is an ordinary word, since every word typed has to appear",
			query:        "pane or marker",
			wantPositive: []string{"pane", "or", "marker"},
		},
		{
			name:         "a query of nothing but an exclusion asks for no words at all",
			query:        "-zeta",
			wantNegative: []string{"zeta"},
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
			name:         "a minus inside a word is part of it",
			query:        "well-known",
			wantPositive: []string{"well-known"},
		},
		{
			name:         "only a single minus excludes, so a flag name is a word",
			query:        "--config",
			wantPositive: []string{"--config"},
		},
		{
			name:         "a quoted minus is part of the word rather than an exclusion",
			query:        `"-config"`,
			wantPositive: []string{"-config"},
		},
		{
			name:         "a minus on its own is not an exclusion of the next word",
			query:        "- config",
			wantPositive: []string{"config"},
		},
		{
			name:         "a trailing minus asks for nothing",
			query:        "config -",
			wantPositive: []string{"config"},
		},
		{
			name:         "a quoted phrase behind a minus is still excluded",
			query:        `pane -"derives markers"`,
			wantPositive: []string{"pane"},
			wantNegative: []string{"derives markers"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitTerms(tt.query)
			assert.Equal(t, tt.wantPositive, got.positive)
			assert.Equal(t, tt.wantNegative, got.negative)
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
	*Store
	agents   *agentstore.Store
	workflow string
}

func open(t *testing.T) *fixture {
	t.Helper()
	p, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	workflows := workflowstore.New(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default"})
	require.NoError(t, err)

	return &fixture{
		Store:    New(p.GetDB()),
		agents:   agentstore.New(p.GetDB()),
		workflow: created.ID,
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
