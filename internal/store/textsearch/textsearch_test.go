package textsearch_test

import (
	"testing"

	textsearchstore "komrad/internal/store/textsearch"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
)

// TestMark covers the marking a substring-only hit gets on this side,
// where the offsets of the match have to be offsets into the snippet the caller
// is handed back.
func TestMark(t *testing.T) {
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
			want:    "talking about " + textsearchstore.MarkOpen + "postgres" + textsearchstore.MarkClose + " today",
		},
		{
			name:    "every occurrence is wrapped",
			snippet: "postgres and postgres",
			terms:   []string{"postgres"},
			want:    textsearchstore.MarkOpen + "postgres" + textsearchstore.MarkClose + " and " + textsearchstore.MarkOpen + "postgres" + textsearchstore.MarkClose,
		},
		{
			name:    "a match of another case keeps the casing the snippet had",
			snippet: "talking about Postgres today",
			terms:   []string{"postgres"},
			want:    "talking about " + textsearchstore.MarkOpen + "Postgres" + textsearchstore.MarkClose + " today",
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
			want:    "o" + textsearchstore.MarkOpen + "k" + textsearchstore.MarkClose,
		},
		{
			name:    "a query that folds to more bytes marks only the bytes it matched",
			snippet: "bitİ",
			terms:   []string{"tİ"},
			want:    "bi" + textsearchstore.MarkOpen + "tİ" + textsearchstore.MarkClose,
		},
		{
			name:    "several terms are each wrapped where they fall",
			snippet: "the pgxpool tuning notes",
			terms:   []string{"pool", "tuning"},
			want: "the pgx" + textsearchstore.MarkOpen + "pool" + textsearchstore.MarkClose + " " +
				textsearchstore.MarkOpen + "tuning" + textsearchstore.MarkClose + " notes",
		},
		{
			name:    "overlapping terms are marked once, the longer of the two",
			snippet: "the pgxpool wrapper",
			terms:   []string{"pgx", "pgxpool"},
			want:    "the " + textsearchstore.MarkOpen + "pgxpool" + textsearchstore.MarkClose + " wrapper",
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
			assert.Equal(t, tt.want, textsearchstore.Mark(tt.snippet, tt.terms))
		})
	}
}

// TestSplit covers the words pulled out of a query for the substring fallback, which
// matches text as it was typed and so has to be handed the terms rather than the operators.
func TestSplit(t *testing.T) {
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
			got := textsearchstore.Split(tt.query)
			assert.Equal(t, tt.wantWanted, got.Wanted)
			assert.Equal(t, tt.wantExcluded, got.Excluded)
		})
	}
}

// TestQuote covers how the words a person typed are handed to the database.
func TestQuote(t *testing.T) {
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
			assert.Equal(t, tt.want, textsearchstore.Quote(tt.terms, tt.prefix))
		})
	}
}

// TestPhrase covers the words a query is tested whole for.
func TestPhrase(t *testing.T) {
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
			assert.Equal(t, tt.want, textsearchstore.Phrase(tt.terms))
		})
	}
}

// TestHasNear covers which queries the substring fallback is run for.
func TestHasNear(t *testing.T) {
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
			assert.Equal(t, tt.want, textsearchstore.HasNear(tt.terms))
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
			assert.Equal(t, tt.want, textsearchstore.LikePattern(tt.query))
		})
	}
}
