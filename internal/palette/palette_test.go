package palette

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoesMatch(t *testing.T) {
	tests := []struct {
		name  string
		item  string
		query string
		want  bool
	}{
		{
			name:  "a letter the name does not hold matches nothing",
			item:  "open-pr",
			query: "z",
			want:  false,
		},
		{
			name:  "letters in the wrong order do not match",
			item:  "open-pr",
			query: "rp",
			want:  false,
		},
		{
			name:  "letters spread through the name match as a subsequence",
			item:  "worktree-push",
			query: "wtp",
			want:  true,
		},
		{
			name:  "case is ignored",
			item:  "Open-PR",
			query: "opr",
			want:  true,
		},
		{
			name:  "an empty query keeps the name",
			item:  "open-pr",
			query: "",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, doesMatch(tt.item, tt.query))
		})
	}
}

// TestTypingNarrowsTheList covers the one thing the palette does that a tmux menu cannot,
// and that the cursor lands somewhere the next enter can act on.
func TestTypingNarrowsTheList(t *testing.T) {
	shown := press(t, newModel(RunParams{Items: []Item{
		{Name: "open-pr", Executable: true},
		{Name: "worktree-push", Executable: true},
		{Name: "run-tests", Executable: true},
	}}), "w", "t")

	require.Len(t, shown.rows(), 1)
	assert.Equal(t, "worktree-push", shown.rows()[0].Name)
	assert.Equal(t, 0, shown.cursor, "the cursor sits on a row the narrowed list still draws")
}

// TestEnterFiresTheHoveredAction covers what the popup is for: the action under the cursor
// runs on the agent the palette was opened over, and the popup closes.
func TestEnterFiresTheHoveredAction(t *testing.T) {
	var fired []string

	params := RunParams{
		Agent: "auth",
		Items: []Item{
			{Name: "open-pr", Executable: true},
			{Name: "run-tests", Executable: true},
		},
		Fire: func(name string) error {
			fired = append(fired, name)

			return nil
		},
	}

	shown := press(t, newModel(params), "down", "enter")

	assert.Equal(t, []string{"run-tests"}, fired)
	assert.NoError(t, shown.failure)
}

// TestFiringAFailedActionCarriesTheError covers the popup being held open on a refusal,
// which is the caller's non-zero exit and so has to survive the program ending.
func TestFiringAFailedActionCarriesTheError(t *testing.T) {
	refused := errors.New("the file is not executable")

	shown := press(t, newModel(RunParams{
		Items: []Item{{Name: "open-pr"}},
		Fire:  func(string) error { return refused },
	}), "enter")

	assert.ErrorIs(t, shown.failure, refused)
}

// TestEnterOnNothingFiresNothing covers a query that matched no action, where the cursor is
// on a row that is not there.
func TestEnterOnNothingFiresNothing(t *testing.T) {
	fired := 0

	shown := press(t, newModel(RunParams{
		Items: []Item{{Name: "open-pr", Executable: true}},
		Fire: func(string) error {
			fired++

			return nil
		},
	}), "z", "enter")

	assert.Zero(t, fired)
	assert.NoError(t, shown.failure)
}

// TestViewDrawsTheAgentAndWhatIsHovered covers the popup saying what it is about to act on,
// since the pane it was opened over is the only thing that decided that.
func TestViewDrawsTheAgentAndWhatIsHovered(t *testing.T) {
	shown := press(t, newModel(RunParams{
		Agent: "auth",
		Items: []Item{
			{Name: "open-pr", Description: "opens the pull request", Executable: true},
			{Name: "run-tests", Executable: true},
		},
	}))

	view := shown.View()
	assert.Contains(t, view, "auth")
	assert.Contains(t, view, "open-pr")
	assert.Contains(t, view, "opens the pull request")

	moved := press(t, shown, "down")
	assert.Contains(t, moved.View(), "run-tests", "an action declaring nothing falls back to its name")
}

// TestListScrollsToTheCursor covers a list longer than the popup, since the row being fired
// has to be one that is drawn.
func TestListScrollsToTheCursor(t *testing.T) {
	items := make([]Item, 0, 30)
	for i := range 30 {
		items = append(items, Item{Name: string(rune('a'+i)) + "-action", Executable: true})
	}

	shown := press(t, newModel(RunParams{Items: items}), "down", "down", "down", "down", "down")
	shown.height = chromeLines + 3

	view := shown.View()
	assert.Contains(t, view, items[5].Name, "the row under the cursor is drawn")
	assert.NotContains(t, view, items[0].Name+"\n", "a row scrolled past is not")
}

// press feeds keys to a model the way bubbletea would and returns what they left behind.
func press(t *testing.T, start model, keys ...string) model {
	t.Helper()

	current := tea.Model(start)
	for _, key := range keys {
		next, _ := current.Update(keyMsg(key))
		current = next
	}

	shown, ok := current.(model)
	require.True(t, ok)

	return shown
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.ToLower(key))}
	}
}
