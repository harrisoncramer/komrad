package searchsyntax_test

import (
	"testing"
	"time"

	"komrad/internal/searchsyntax"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var grammar = searchsyntax.Grammar{
	Keys: []searchsyntax.Key{
		{Name: "repo"},
		{Name: "is", Negatable: true, Repeatable: true},
	},
}

// TestParse covers how a query splits into words and clauses and the clauses it refuses.
func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		strict  bool
		want    searchsyntax.Query
		wantErr string
	}{
		{
			name:    "a qualifier with no value",
			text:    "repo:",
			wantErr: "repo: needs a value",
		},
		{
			name:    "a qualifier given twice",
			text:    "repo:a repo:b",
			wantErr: "repo: is given twice",
		},
		{
			name:    "a negated qualifier that has no opposite",
			text:    "-repo:a",
			wantErr: "repo: cannot be negated",
		},
		{
			name: "words alone stay words",
			text: `reconciler -"stale pane" --force`,
			want: searchsyntax.Query{Words: []string{"reconciler", `-"stale pane"`, "--force"}},
		},
		{
			name: "clauses beside words, keys in any case",
			text: "IS:dirty fix -is:paused Repo:komrad",
			want: searchsyntax.Query{
				Words: []string{"fix"},
				Clauses: []searchsyntax.Clause{
					{Key: "is", Value: "dirty"},
					{Key: "is", Value: "paused", Negated: true},
					{Key: "repo", Value: "komrad"},
				},
			},
		},
		{
			name: "a quoted value holds a space",
			text: `repo:"komrad web"`,
			want: searchsyntax.Query{Clauses: []searchsyntax.Clause{{Key: "repo", Value: "komrad web"}}},
		},
		{
			name: "a key the grammar lacks stays a word",
			text: "http://localhost:5460 state:idle",
			want: searchsyntax.Query{Words: []string{"http://localhost:5460", "state:idle"}},
		},
		{
			name: "a quoted qualifier is a phrase",
			text: `"repo:a"`,
			want: searchsyntax.Query{Words: []string{`"repo:a"`}},
		},
		{
			name:    "a key a strict grammar lacks",
			text:    "fix state:idle",
			strict:  true,
			wantErr: "state: is not a qualifier here",
		},
		{
			name:   "a strict grammar still takes quoted and keyless colons as words",
			text:   `"state:idle" :8787`,
			strict: true,
			want:   searchsyntax.Query{Words: []string{`"state:idle"`, ":8787"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strictness := grammar
			strictness.Strict = tt.strict

			got, err := searchsyntax.Parse(tt.text, strictness)
			if tt.wantErr != "" {
				require.ErrorIs(t, err, searchsyntax.ErrBadQualifier)
				assert.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, &tt.want, got)
		})
	}
}

// TestParseTimes covers the days and spans since:, until: and on: take.
func TestParseTimes(t *testing.T) {
	zone := time.FixedZone("EDT", -4*60*60)
	now := time.Date(2026, 9, 27, 15, 30, 0, 0, zone)
	midnight := time.Date(2026, 9, 27, 0, 0, 0, 0, zone)

	tests := []struct {
		name    string
		parse   func(key, value string, now time.Time) (time.Time, error)
		value   string
		want    time.Time
		wantErr string
	}{
		{
			name:  "a span in minutes",
			parse: searchsyntax.ParseStart,
			value: "30m",
			want:  now.Add(-30 * time.Minute),
		},
		{
			name:  "a span in weeks",
			parse: searchsyntax.ParseStart,
			value: "2W",
			want:  now.Add(-14 * 24 * time.Hour),
		},
		{
			name:  "the start of today",
			parse: searchsyntax.ParseStart,
			value: "today",
			want:  midnight,
		},
		{
			name:  "the end of yesterday",
			parse: searchsyntax.ParseEnd,
			value: "yesterday",
			want:  midnight,
		},
		{
			name:  "a span ends where it starts",
			parse: searchsyntax.ParseEnd,
			value: "2h",
			want:  now.Add(-2 * time.Hour),
		},
		{
			name:  "a day by date",
			parse: searchsyntax.ParseDay,
			value: "2026-09-20",
			want:  time.Date(2026, 9, 20, 0, 0, 0, 0, zone),
		},
		{
			name:    "a value that does not parse",
			parse:   searchsyntax.ParseStart,
			value:   "yesterdy",
			wantErr: "since:yesterdy is not a day",
		},
		{
			name:    "a span too long to count back from now",
			parse:   searchsyntax.ParseStart,
			value:   "9999999999999w",
			wantErr: "since:9999999999999w is not a day",
		},
		{
			name:    "a span is not one day",
			parse:   searchsyntax.ParseDay,
			value:   "3d",
			wantErr: "since:3d is not a day",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.parse("since", tt.value, now)
			if tt.wantErr != "" {
				require.ErrorIs(t, err, searchsyntax.ErrBadQualifier)
				assert.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
