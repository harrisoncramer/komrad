package paint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTrimLastRune pins that a backspace drops a whole rune rather than one byte of a
// multi-byte one.
func TestTrimLastRune(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "an empty string",
			in:   "",
			want: "",
		},
		{
			name: "a multi-byte rune is dropped whole rather than half-deleted",
			in:   "aé",
			want: "a",
		},
		{
			name: "an ascii string",
			in:   "abc",
			want: "ab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, TrimLastRune(tt.in))
		})
	}
}

// TestMarkedTerms covers the words a highlight is looked for by, which are read back off the
// snippet.
func TestMarkedTerms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "nothing marked names no word",
			in:   "a turn that matched on a stem",
		},
		{
			name: "every marked word is named once",
			in:   "the " + markText("Sandbox") + " and the " + markText("sandbox") + " and " + markText("tenant"),
			want: []string{"sandbox", "tenant"},
		},
		{
			name: "a word with a capital outside ASCII keeps it, since the transcript is folded the same way",
			in:   markText("Ökonomie"),
			want: []string{"Ökonomie"},
		},
		{
			name: "an unclosed mark is dropped",
			in:   "the " + MarkOpen + "sandbox",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MarkedTerms(tt.in))
		})
	}
}

// TestMatchSpans covers where the highlight goes, which the search index does not say: it
// gives the turn and the words, so the words are found within the turn here.
func TestMatchSpans(t *testing.T) {
	const content = "a sandbox up top\n\nthe Sandbox tenant\nruns sandboxed\n\nsandbox down below"

	turn := int64(strings.Index(content, "the Sandbox"))
	turnLen := int64(len("the Sandbox tenant\nruns sandboxed"))

	at := func(from int, word string) Span {
		i := from + strings.Index(content[from:], word)

		return Span{Start: i, End: i + len(word)}
	}

	tests := []struct {
		name   string
		offset int64
		length int64
		terms  []string
		want   []Span
	}{
		{
			name:   "every spelling of the word within the turn is marked",
			offset: turn,
			length: turnLen,
			terms:  []string{"sandbox"},
			want:   []Span{at(int(turn), "Sandbox"), at(strings.Index(content, "runs "), "sandbox")},
		},
		{
			name:   "the turns around it are left alone",
			offset: turn,
			length: turnLen,
			terms:  []string{"top"},
		},
		{
			name:   "two of the words spelled over the same letters merge",
			offset: turn,
			length: turnLen,
			terms:  []string{"sandbox", "sandboxed"},
			want:   []Span{at(int(turn), "Sandbox"), at(strings.Index(content, "runs "), "sandboxed")},
		},
		{
			name:   "the word inside a longer word is left alone",
			offset: turn,
			length: turnLen,
			terms:  []string{"and"},
		},
		{
			name:   "no word to look for marks nothing",
			offset: turn,
			length: turnLen,
		},
		{
			name:   "a turn past the end of the transcript marks nothing",
			offset: int64(len(content) + 1),
			length: turnLen,
			terms:  []string{"sandbox"},
		},
		{
			name:   "a turn running past the end stops at it",
			offset: turn,
			length: turnLen * 4,
			terms:  []string{"tenant", "down"},
			want:   []Span{at(int(turn), "tenant"), at(int(turn), "down")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MatchSpans(content, tt.offset, tt.length, tt.terms))
		})
	}
}

// TestMatchSpansMarksWholeWords pins the front of the match being a word boundary, which is
// what the listing's own snippet marks, with the punctuation these transcripts are written
// with reaching well past ASCII.
func TestMatchSpansMarksWholeWords(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "a word of its own is marked",
			content: "the sandbox here",
			want:    true,
		},
		{
			name:    "a curly quote in front of it is not part of the word",
			content: "he said \u201csandbox\u201d once",
			want:    true,
		},
		{
			name:    "an em dash in front of it is not part of the word",
			content: "the thing\u2014sandbox\u2014here",
			want:    true,
		},
		{
			name:    "a letter outside ASCII in front of it is part of the word",
			content: "the Gr\u00fcnsandbox here",
		},
		{
			name:    "a letter in front of it is part of the word",
			content: "the presandbox here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := MatchSpans(tt.content, 0, int64(len(tt.content)), []string{"sandbox"})

			if !tt.want {
				assert.Empty(t, spans, "the word is only spelled inside a longer one")

				return
			}

			word := strings.Index(tt.content, "sandbox")
			assert.Equal(t, []Span{{Start: word, End: word + len("sandbox")}}, spans)
		})
	}
}

// TestMatchSpansFoldsLikeTheTermsDo pins the two sides of the search being folded the same way:
// a word the snippet marks with a capital outside ASCII is still found in the transcript, which
// a Unicode fold on one side only would miss.
func TestMatchSpansFoldsLikeTheTermsDo(t *testing.T) {
	const content = "the Ökonomie report"

	terms := MarkedTerms("the " + markText("Ökonomie") + " report")
	word := strings.Index(content, "Ökonomie")

	assert.Equal(t, []Span{{Start: word, End: word + len("Ökonomie")}},
		MatchSpans(content, 0, int64(len(content)), terms))
}

func markText(text string) string {
	return MarkOpen + text + MarkClose
}
