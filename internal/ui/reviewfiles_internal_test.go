package ui

import (
	"testing"

	"komrad/internal/diff"

	"github.com/charmbracelet/x/ansi"
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

// TestFormatStatColumns covers a file's counts right aligned in the tree's columns.
func TestFormatStatColumns(t *testing.T) {
	columns := statColumns{Added: 4, Removed: 3}
	tests := []struct {
		name string
		stat diff.Stat
		want string
	}{
		{
			name: "a file touching both sides fills both columns",
			stat: diff.Stat{Added: 120, Removed: 12},
			want: "+120 −12",
		},
		{
			name: "a narrower count pads to its column",
			stat: diff.Stat{Added: 3, Removed: 1},
			want: "  +3  −1",
		},
		{
			name: "a side the file did not touch keeps its column blank",
			stat: diff.Stat{Added: 7},
			want: "  +7    ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ansi.Strip(formatStatColumns(tt.stat, columns)))
		})
	}
}
