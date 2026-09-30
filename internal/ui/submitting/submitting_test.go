package submitting_test

import (
	"testing"
	"time"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/submitting"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestNotice covers the line under a held form, counting once the write is slow.
func TestNotice(t *testing.T) {
	t.Parallel()

	since := time.Now()

	tests := []struct {
		name  string
		write submitting.Write
		now   time.Time
		width int
		want  string
	}{
		{
			name:  "a write with no start stands still",
			write: submitting.Write{Doing: "creating agent foo"},
			now:   since,
			want:  "⠋ creating agent foo…",
		},
		{
			name:  "a quick write stands still",
			write: submitting.Write{Doing: "creating agent foo", Since: since},
			now:   since.Add(time.Second),
			want:  "⠋ creating agent foo…",
		},
		{
			name:  "a slow write counts its seconds",
			write: submitting.Write{Doing: "creating agent foo", Since: since},
			now:   since.Add(12 * time.Second),
			want:  "⠋ creating agent foo… 12s · esc to keep working",
		},
		{
			name:  "a very slow write counts its minutes",
			write: submitting.Write{Doing: "creating agent foo", Since: since},
			now:   since.Add(400 * time.Second),
			want:  "⠋ creating agent foo… 6m40s · esc to keep working",
		},
		{
			name:  "a narrow form drops the hint before the count",
			write: submitting.Write{Doing: "creating agent foo", Since: since},
			now:   since.Add(12 * time.Second),
			width: 30,
			want:  "⠋ creating agent foo… 12s",
		},
		{
			name:  "a very narrow form clips the label",
			write: submitting.Write{Doing: "creating agent foo", Since: since},
			now:   since.Add(12 * time.Second),
			width: 10,
			want:  "⠋ creatin…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.write.Notice(submitting.Params{Palette: paint.Panel, Frame: "⠋", Now: tt.now, Width: tt.width})
			assert.Equal(t, tt.want, ansi.Strip(got))
		})
	}
}

// TestBadge covers the newest write in flight drawn in the sidebar's corner.
func TestBadge(t *testing.T) {
	t.Parallel()

	since := time.Now()

	tests := []struct {
		name   string
		writes []submitting.Write
		width  int
		want   string
	}{
		{
			name: "nothing in flight draws nothing",
			want: "",
		},
		{
			name:   "one write is named",
			writes: []submitting.Write{{Doing: "archiving foo", Since: since}},
			want:   "⠋ archiving foo…",
		},
		{
			name: "several writes name the newest and count the rest",
			writes: []submitting.Write{
				{Doing: "archiving foo", Since: since},
				{Doing: "restoring bar", Since: since.Add(time.Second)},
				{Doing: "pausing baz", Since: since.Add(-time.Second)},
			},
			want: "⠋ restoring bar… +2",
		},
		{
			name:   "a narrow corner clips the label",
			writes: []submitting.Write{{Doing: "archiving foo", Since: since}},
			width:  8,
			want:   "⠋ archi…",
		},
		{
			name: "a corner with no room for a label keeps the spinner and the count",
			writes: []submitting.Write{
				{Doing: "archiving foo", Since: since},
				{Doing: "restoring bar", Since: since},
			},
			width: 5,
			want:  "⠋ +1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := submitting.Badge(tt.writes, submitting.Params{Palette: paint.Panel, Frame: "⠋", Now: since, Width: tt.width})
			assert.Equal(t, tt.want, ansi.Strip(got))
		})
	}
}
