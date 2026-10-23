package menu_test

import (
	"testing"

	"komrad/internal/ui/menu"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFirstOn checks the cursor opens on the first entry that would do something.
func TestFirstOn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []menu.Entry
		want    int
	}{
		{
			name: "no entries",
			want: menu.Cancel,
		},
		{
			name:    "every entry off",
			entries: []menu.Entry{{IsDimmed: true}, {IsDimmed: true}},
			want:    menu.Cancel,
		},
		{
			name:    "skips entries that are off",
			entries: []menu.Entry{{IsDimmed: true}, {Label: "on"}},
			want:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, menu.Menu{Entries: tt.entries}.FirstOn())
		})
	}
}

// TestNested checks a nested entry is off only when nothing inside it would do anything.
func TestNested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		menu    menu.Menu
		wantOff bool
	}{
		{
			name: "a menu with an entry on",
			menu: menu.Menu{Entries: []menu.Entry{{Label: "on"}}},
		},
		{
			name:    "a menu with every entry off",
			menu:    menu.Menu{Entries: []menu.Entry{{IsDimmed: true}}},
			wantOff: true,
		},
		{
			name: "a list with no entries yet",
			menu: menu.Menu{ActionsForChoice: func(int) menu.Menu { return menu.Menu{} }},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			entry := menu.Nested("git", nil, tt.menu)

			assert.Equal(t, tt.wantOff, entry.IsDimmed)
			assert.NotNil(t, entry.Submenu)
		})
	}
}

// TestWithNestedLeavesTheBandItCameFrom checks two bands built from one never share entries.
func TestWithNestedLeavesTheBandItCameFrom(t *testing.T) {
	t.Parallel()

	entries := make([]menu.Entry, 1, 2)
	entries[0] = menu.Entry{Label: "logs"}
	band := menu.Menu{Entries: entries}

	first := menu.WithNested(band, "jump", nil, menu.Menu{Entries: []menu.Entry{{Label: "agents"}}})
	second := menu.WithNested(band, "git", nil, menu.Menu{Entries: []menu.Entry{{Label: "push"}}})

	require.Len(t, first.Entries, 2)
	require.Len(t, second.Entries, 2)
	assert.Equal(t, "jump", first.Entries[1].Label)
	assert.Equal(t, "git", second.Entries[1].Label)
	assert.Len(t, band.Entries, 1)
}

// TestJoin checks two bands join under the top title with a rule between them.
func TestJoin(t *testing.T) {
	t.Parallel()

	joined := menu.Join(
		menu.Menu{Title: "agent", Entries: []menu.Entry{{Label: "logs"}}},
		menu.Menu{Title: "task", Entries: []menu.Entry{{Label: "archive"}}},
	)

	assert.Equal(t, "agent", joined.Title)
	require.Len(t, joined.Entries, 3)
	assert.True(t, joined.Entries[1].IsDivider)
	assert.True(t, joined.Entries[1].IsDimmed)
	assert.Equal(t, "archive", joined.Entries[2].Label)
}

// TestDrawnOff checks an entry is drawn off only when there is a reason to say.
func TestDrawnOff(t *testing.T) {
	t.Parallel()

	entry := menu.Entry{Label: "push"}

	assert.Equal(t, entry, menu.DrawnOff(entry, ""))
	assert.Equal(t, menu.Entry{Label: "push", IsDimmed: true, RefusalReason: "nothing to push"}, menu.DrawnOff(entry, "nothing to push"))
}
