package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"komrad/internal/ui/paint"
)

// TestSaidRows covers how the turns a says: search found are wrapped to the column and marked where the server matched.
func TestSaidRows(t *testing.T) {
	tests := []struct {
		name     string
		snippets []string
		width    int
		want     []string
	}{
		{
			name:     "a word the server matched by its stem is marked though it differs from the query",
			snippets: []string{"kept \x02running\x03 the tests"},
			width:    40,
			want:     []string{paint.Panel.HighlightStyled("kept running the tests", []paint.Span{{Start: 5, End: 12}})},
		},
		{
			name:     "a marked phrase split across a wrap is marked on both rows",
			snippets: []string{"ab \x02cd ef\x03 gh"},
			width:    5,
			want: []string{
				paint.Panel.HighlightStyled("ab cd", []paint.Span{{Start: 3, End: 5}}),
				paint.Panel.HighlightStyled("ef gh", []paint.Span{{Start: 0, End: 2}}),
			},
		},
		{
			name:     "a near hit the server marked nothing in is drawn plain",
			snippets: []string{"see internal/xylophonestore"},
			width:    40,
			want:     []string{paint.Panel.HighlightStyled("see internal/xylophonestore", []paint.Span{})},
		},
		{
			name:     "turns are set apart by a blank row and their lines run together",
			snippets: []string{"\x02one\x03", "two\n\x02one\x03"},
			width:    40,
			want: []string{
				paint.Panel.HighlightStyled("one", []paint.Span{{Start: 0, End: 3}}),
				"",
				paint.Panel.HighlightStyled("two one", []paint.Span{{Start: 4, End: 7}}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, saidRows(tt.snippets, tt.width))
		})
	}
}
