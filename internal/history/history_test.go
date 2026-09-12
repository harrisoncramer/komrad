package history_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"weave/internal/history"
)

func goBackTimes(list *history.List[string], times int) {
	for range times {
		list.GoBack()
	}
}

// TestVisit walks what arriving somewhere does to the list, the degenerate cases first.
func TestVisit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		visits      []string
		wantEntries []string
		wantCurrent string
	}{
		{
			name:        "nothing visited leaves the list empty",
			visits:      nil,
			wantEntries: nil,
			wantCurrent: "",
		},
		{
			name:        "the same place twice in a row is one entry",
			visits:      []string{"a", "a"},
			wantEntries: []string{"a"},
			wantCurrent: "a",
		},
		{
			name:        "bouncing between two places keeps two entries",
			visits:      []string{"a", "b", "a", "b"},
			wantEntries: []string{"a", "b"},
			wantCurrent: "b",
		},
		{
			name:        "revisiting an older place moves it to the end",
			visits:      []string{"a", "b", "c", "a"},
			wantEntries: []string{"b", "c", "a"},
			wantCurrent: "a",
		},
		{
			name:        "distinct places are kept in order",
			visits:      []string{"a", "b", "c"},
			wantEntries: []string{"a", "b", "c"},
			wantCurrent: "c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			list := history.New[string](0)
			for _, visit := range tt.visits {
				list.Visit(visit)
			}

			assert.Equal(t, tt.wantEntries, list.GetEntries())
			assert.Equal(t, len(tt.wantEntries), list.Len())

			current, ok := list.GetCurrent()
			assert.Equal(t, tt.wantCurrent != "", ok)
			assert.Equal(t, tt.wantCurrent, current)
		})
	}
}

// TestGoBack walks stepping backwards, the cases with nowhere to go first.
func TestGoBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		visits    []string
		backs     int
		wantEntry string
		wantOK    bool
	}{
		{
			name:      "an empty list has nowhere to go",
			visits:    nil,
			backs:     1,
			wantEntry: "",
			wantOK:    false,
		},
		{
			name:      "a single entry has nothing behind it",
			visits:    []string{"a"},
			backs:     1,
			wantEntry: "",
			wantOK:    false,
		},
		{
			name:      "stepping past the start stops at the start",
			visits:    []string{"a", "b"},
			backs:     2,
			wantEntry: "",
			wantOK:    false,
		},
		{
			name:      "bouncing by hand still steps back to a distinct place",
			visits:    []string{"a", "b", "a", "b"},
			backs:     1,
			wantEntry: "a",
			wantOK:    true,
		},
		{
			name:      "three steps back over three places reach the oldest",
			visits:    []string{"a", "b", "c", "d"},
			backs:     3,
			wantEntry: "a",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			list := history.New[string](0)
			for _, visit := range tt.visits {
				list.Visit(visit)
			}

			goBackTimes(list, tt.backs-1)

			entry, ok := list.GoBack()
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantEntry, entry)
		})
	}
}

// TestGoForward walks stepping forwards, the cases with nowhere to go first.
func TestGoForward(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		visits    []string
		backs     int
		forwards  int
		wantEntry string
		wantOK    bool
	}{
		{
			name:      "an empty list has nowhere to go",
			visits:    nil,
			backs:     0,
			forwards:  1,
			wantEntry: "",
			wantOK:    false,
		},
		{
			name:      "the most recent place has nothing ahead of it",
			visits:    []string{"a", "b"},
			backs:     0,
			forwards:  1,
			wantEntry: "",
			wantOK:    false,
		},
		{
			name:      "stepping past the end stops at the end",
			visits:    []string{"a", "b", "c"},
			backs:     1,
			forwards:  2,
			wantEntry: "",
			wantOK:    false,
		},
		{
			name:      "forward undoes a step back",
			visits:    []string{"a", "b", "c"},
			backs:     2,
			forwards:  1,
			wantEntry: "b",
			wantOK:    true,
		},
		{
			name:      "forward walks back to where the stepping began",
			visits:    []string{"a", "b", "c"},
			backs:     2,
			forwards:  2,
			wantEntry: "c",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			list := history.New[string](0)
			for _, visit := range tt.visits {
				list.Visit(visit)
			}

			goBackTimes(list, tt.backs)

			for range tt.forwards - 1 {
				list.GoForward()
			}

			entry, ok := list.GoForward()
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantEntry, entry)
		})
	}
}

// TestVisitAfterGoBack pins which forward entries survive arriving somewhere mid-walk.
func TestVisitAfterGoBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		visits      []string
		backs       int
		then        string
		wantEntries []string
		wantCurrent string
		wantForward bool
	}{
		{
			name:        "arriving where you already are keeps the forward entries",
			visits:      []string{"a", "b", "c"},
			backs:       1,
			then:        "b",
			wantEntries: []string{"a", "b", "c"},
			wantCurrent: "b",
			wantForward: true,
		},
		{
			name:        "arriving somewhere new drops the forward entries",
			visits:      []string{"a", "b", "c"},
			backs:       2,
			then:        "d",
			wantEntries: []string{"a", "d"},
			wantCurrent: "d",
			wantForward: false,
		},
		{
			name:        "arriving at a forward entry drops the rest of them",
			visits:      []string{"a", "b", "c"},
			backs:       2,
			then:        "c",
			wantEntries: []string{"a", "c"},
			wantCurrent: "c",
			wantForward: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			list := history.New[string](0)
			for _, visit := range tt.visits {
				list.Visit(visit)
			}

			goBackTimes(list, tt.backs)
			list.Visit(tt.then)

			assert.Equal(t, tt.wantEntries, list.GetEntries())

			current, ok := list.GetCurrent()
			require.True(t, ok)
			assert.Equal(t, tt.wantCurrent, current)

			_, ok = list.GoForward()
			assert.Equal(t, tt.wantForward, ok)
		})
	}
}

// TestRemove pins where the cursor lands once an entry is forgotten.
func TestRemove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		visits      []string
		backs       int
		remove      string
		wantRemoved bool
		wantEntries []string
		wantCurrent string
	}{
		{
			name:        "removing what was never visited changes nothing",
			visits:      []string{"a", "b"},
			backs:       0,
			remove:      "c",
			wantRemoved: false,
			wantEntries: []string{"a", "b"},
			wantCurrent: "b",
		},
		{
			name:        "removing behind the cursor keeps the current entry",
			visits:      []string{"a", "b", "c"},
			backs:       0,
			remove:      "a",
			wantRemoved: true,
			wantEntries: []string{"b", "c"},
			wantCurrent: "c",
		},
		{
			name:        "removing ahead of the cursor keeps the current entry",
			visits:      []string{"a", "b", "c"},
			backs:       2,
			remove:      "c",
			wantRemoved: true,
			wantEntries: []string{"a", "b"},
			wantCurrent: "a",
		},
		{
			name:        "removing the current entry lands on the one after it",
			visits:      []string{"a", "b", "c"},
			backs:       1,
			remove:      "b",
			wantRemoved: true,
			wantEntries: []string{"a", "c"},
			wantCurrent: "c",
		},
		{
			name:        "removing the last entry lands on the one before it",
			visits:      []string{"a", "b", "c"},
			backs:       0,
			remove:      "c",
			wantRemoved: true,
			wantEntries: []string{"a", "b"},
			wantCurrent: "b",
		},
		{
			name:        "removing the only entry empties the list",
			visits:      []string{"a"},
			backs:       0,
			remove:      "a",
			wantRemoved: true,
			wantEntries: nil,
			wantCurrent: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			list := history.New[string](0)
			for _, visit := range tt.visits {
				list.Visit(visit)
			}

			goBackTimes(list, tt.backs)

			assert.Equal(t, tt.wantRemoved, list.Remove(tt.remove))
			assert.Equal(t, tt.wantEntries, list.GetEntries())

			current, ok := list.GetCurrent()
			assert.Equal(t, tt.wantCurrent != "", ok)
			assert.Equal(t, tt.wantCurrent, current)
		})
	}
}

// TestLimit shows a full list dropping its oldest entries rather than refusing new ones.
func TestLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		limit       int
		visits      []string
		backs       int
		then        string
		wantEntries []string
		wantCurrent string
		wantBack    bool
	}{
		{
			name:        "a limit of one holds only the latest place",
			limit:       1,
			visits:      []string{"a", "b"},
			backs:       0,
			then:        "",
			wantEntries: []string{"b"},
			wantCurrent: "b",
			wantBack:    false,
		},
		{
			name:        "a straight run past the limit drops the oldest",
			limit:       3,
			visits:      []string{"a", "b", "c", "d", "e"},
			backs:       0,
			then:        "",
			wantEntries: []string{"c", "d", "e"},
			wantCurrent: "e",
			wantBack:    true,
		},
		{
			name:        "arriving mid-walk drops the forward entries before the limit applies",
			limit:       2,
			visits:      []string{"a", "b", "c"},
			backs:       1,
			then:        "d",
			wantEntries: []string{"b", "d"},
			wantCurrent: "d",
			wantBack:    true,
		},
		{
			name:        "revisiting inside a full list reorders it rather than trimming it",
			limit:       3,
			visits:      []string{"a", "b", "c"},
			backs:       0,
			then:        "a",
			wantEntries: []string{"b", "c", "a"},
			wantCurrent: "a",
			wantBack:    true,
		},
		{
			name:        "revisiting the middle of a full list keeps every entry",
			limit:       3,
			visits:      []string{"a", "b", "c"},
			backs:       0,
			then:        "b",
			wantEntries: []string{"a", "c", "b"},
			wantCurrent: "b",
			wantBack:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			list := history.New[string](tt.limit)
			for _, visit := range tt.visits {
				list.Visit(visit)
			}

			goBackTimes(list, tt.backs)

			if tt.then != "" {
				list.Visit(tt.then)
			}

			assert.Equal(t, tt.wantEntries, list.GetEntries())

			current, ok := list.GetCurrent()
			require.True(t, ok)
			assert.Equal(t, tt.wantCurrent, current)

			_, ok = list.GoBack()
			assert.Equal(t, tt.wantBack, ok)
		})
	}
}

// TestRemoveThenVisit shows the cursor Remove leaves behind deciding where the next arrival lands.
func TestRemoveThenVisit(t *testing.T) {
	t.Parallel()

	list := history.New[string](0)
	for _, visit := range []string{"a", "b", "c"} {
		list.Visit(visit)
	}

	goBackTimes(list, 1)
	require.True(t, list.Remove("b"))

	list.Visit("d")

	assert.Equal(t, []string{"a", "c", "d"}, list.GetEntries())

	current, ok := list.GetCurrent()
	require.True(t, ok)
	assert.Equal(t, "d", current)
}

// TestZeroValue shows a list nobody built with New behaving as an empty one.
func TestZeroValue(t *testing.T) {
	t.Parallel()

	var list history.List[string]

	_, ok := list.GetCurrent()
	require.False(t, ok)

	list.Visit("a")
	list.Visit("b")

	assert.Equal(t, []string{"a", "b"}, list.GetEntries())

	entry, ok := list.GoBack()
	require.True(t, ok)
	assert.Equal(t, "a", entry)
}

// TestIntEntries shows the list holding something other than strings.
func TestIntEntries(t *testing.T) {
	t.Parallel()

	list := history.New[int](0)
	list.Visit(1)
	list.Visit(2)

	entry, ok := list.GoBack()
	require.True(t, ok)
	assert.Equal(t, 1, entry)
}
