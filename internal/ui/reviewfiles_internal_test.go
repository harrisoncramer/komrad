package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFitTreeRow covers a row of the file tree giving up what it draws on the right before its
// name.
func TestFitTreeRow(t *testing.T) {
	tests := []struct {
		name  string
		width int
		want  string
	}{
		{
			name:  "a row narrower than its name cuts the name",
			width: 6,
			want:  "revie…",
		},
		{
			name:  "a row with room for the name alone gives up the badges",
			width: 12,
			want:  "review.go",
		},
		{
			name:  "a row with room for the badges gives up the counts",
			width: 16,
			want:  "review.go     ○2",
		},
		{
			name:  "a row with room for everything draws it",
			width: 24,
			want:  "review.go     +12 −3  ○2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fitTreeRow(fitTreeRowParams{
				Left:   "review.go",
				Stat:   "+12 −3",
				Badges: "○2",
				Width:  tt.width,
			})

			assert.Equal(t, tt.want, got)
		})
	}
}
