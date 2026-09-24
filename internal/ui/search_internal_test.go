package ui

import (
	"strings"
	"testing"

	"komrade/internal/ui/paint"
	"komrade/internal/view"

	"github.com/stretchr/testify/assert"
)

// TestHitSpansCoverTheHoveredTurn covers the turn a highlight is measured from, which is the turn
// the hit names and not the whole transcript.
func TestHitSpansCoverTheHoveredTurn(t *testing.T) {
	const content = "alpha reconcile one\n\nbeta reconcile two\n"

	second := int64(strings.Index(content, "beta"))
	word := strings.Index(content[second:], "reconcile") + int(second)

	tests := []struct {
		name   string
		offset int64
		want   []paint.Span
	}{
		{
			name:   "an offset past the end marks nothing",
			offset: int64(len(content) + 1),
		},
		{
			name:   "an offset below zero marks nothing",
			offset: -1,
		},
		{
			name:   "the hit's own turn is marked",
			offset: second,
			want:   []paint.Span{{Start: word, End: word + len("reconcile")}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit := searchHit{
				agentID: "a1",
				offset:  tt.offset,
				length:  int64(len("beta reconcile two")),
				snippet: markText("reconcile"),
			}

			assert.Equal(t, tt.want, hit.spans(content))
		})
	}
}

// TestAgentSpansCoverEveryTurn covers an agent whose conversation said the query more than once:
// every turn is marked, in the order the transcript reads rather than the order the search ranked
// them.
func TestAgentSpansCoverEveryTurn(t *testing.T) {
	const content = "alpha reconcile one\n\nbeta reconcile two\n"

	first := strings.Index(content, "reconcile")
	second := strings.LastIndex(content, "reconcile")
	turnAt := int64(strings.Index(content, "beta"))

	agent := searchAgent{
		id: "a1",
		turns: []searchHit{
			{
				agentID: "a1",
				offset:  turnAt,
				length:  int64(len("beta reconcile two")),
				snippet: markText("reconcile"),
			},
			{
				agentID: "a1",
				offset:  0,
				length:  int64(len("alpha reconcile one")),
				snippet: markText("reconcile"),
			},
		},
	}

	assert.Equal(t, []paint.Span{
		{Start: first, End: first + len("reconcile")},
		{Start: second, End: second + len("reconcile")},
	}, agent.spans(content))
}

// TestNameMatches covers where a name is marked, which is the query as the name spells it rather
// than as it was typed.
func TestNameMatches(t *testing.T) {
	assert.Equal(t, []view.Span{{Start: 0, End: 4}}, nameMatches("biLLing", " bill "))
	assert.Nil(t, nameMatches("billing", "  "))
	assert.Nil(t, nameMatches("billing", "payouts"))
}

// TestFlattenSnippet covers a snippet read into one line of text with the matched stretches
// located in it.
func TestFlattenSnippet(t *testing.T) {
	tests := []struct {
		name        string
		snippet     string
		wantText    string
		wantMatches []view.Span
	}{
		{
			name:     "nothing marked",
			snippet:  "plain words",
			wantText: "plain words",
		},
		{
			name:     "a close with no open is ignored",
			snippet:  "odd" + paint.MarkClose + " words",
			wantText: "odd words",
		},
		{
			name:        "newlines and runs of spaces are one space",
			snippet:     "the\n\n  " + markText("ledger") + "\tis   here",
			wantText:    "the ledger is here",
			wantMatches: []view.Span{{Start: 4, End: 10}},
		},
		{
			name:        "two matches",
			snippet:     markText("pgx") + " and " + markText("pool"),
			wantText:    "pgx and pool",
			wantMatches: []view.Span{{Start: 0, End: 3}, {Start: 8, End: 12}},
		},
		{
			name:        "a match in bytes rather than runes",
			snippet:     "café " + markText("ledger"),
			wantText:    "café ledger",
			wantMatches: []view.Span{{Start: 6, End: 12}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, matches := flattenSnippet(tt.snippet)

			assert.Equal(t, tt.wantText, text)
			assert.Equal(t, tt.wantMatches, matches)
		})
	}
}

// TestSearchHitLead covers who a turn is said by and where it sits, as the row leads with.
func TestSearchHitLead(t *testing.T) {
	tests := []struct {
		name string
		hit  searchHit
		want string
	}{
		{
			name: "no role at all",
			hit:  searchHit{seq: 4},
			want: "#4",
		},
		{
			name: "a role the panel has no word for",
			hit:  searchHit{role: "system", seq: 0},
			want: "system #0",
		},
		{
			name: "the person prompting",
			hit:  searchHit{role: "user", seq: 518},
			want: "you #518",
		},
		{
			name: "the agent",
			hit:  searchHit{role: "assistant", seq: 1},
			want: "agent #1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.hit.lead())
		})
	}
}

func markText(text string) string {
	return paint.MarkOpen + text + paint.MarkClose
}
