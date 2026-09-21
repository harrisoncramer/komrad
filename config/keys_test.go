package config

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadKeys writes one config file and returns what Load made of it.
func loadKeys(t *testing.T, file string) (*Config, error) {
	t.Helper()

	clearConnectionEnvironment(t)

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(file), 0o600))

	return LoadStack(Stack{Paths: []string{path}})
}

// TestLoadRejectsKeysBubbleteaCannotDeliver covers a binding spelled in a way no press ever
// matches, which today loads and leaves the action dead.
func TestLoadRejectsKeysBubbleteaCannotDeliver(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "a key name in the wrong case",
			file: "[keybindings.agents]\nreview = [\"PgUp\"]\n",
		},
		{
			name: "esc spelled out in full",
			file: "[keybindings.agents]\nrename = [\"escape\"]\n",
		},
		{
			name: "enter spelled as a control key, which bubbletea reports as enter",
			file: "[keybindings.agents]\nrename = [\"ctrl+m\"]\n",
		},
		{
			name: "tab spelled as a control key, which bubbletea reports as tab",
			file: "[keybindings.agents]\nrename = [\"ctrl+i\"]\n",
		},
		{
			name: "space spelled as a word rather than as the space bubbletea reports",
			file: "[keybindings.schedules]\ntoggle = [\"space\"]\n",
		},
		{
			name: "a tmux spelling in a panel binding",
			file: "[keybindings.agents]\nsearch = [\"C-d\"]\n",
		},
		{
			name: "a modifier combination no terminal sends",
			file: "[keybindings.agents]\nmail = [\"ctrl+shift+q\"]\n",
		},
		{
			name: "a function key past the ones bubbletea names",
			file: "[keybindings.agents]\nmail = [\"f25\"]\n",
		},
		{
			name: "an empty key",
			file: "[keybindings.agents]\nsearch = [\"\"]\n",
		},
		{
			name: "a key padded with spaces",
			file: "[keybindings.nav]\ndown = [\" j \"]\n",
		},
		{
			name: "two characters, which arrive as two presses and never match either",
			file: "[keybindings.nav]\ntop = [\"aa\"]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadKeys(t, tt.file)

			require.Error(t, err, "a key no press can produce leaves its action unreachable, so it is a mistake in the file")
		})
	}
}

// TestLoadRejectsAKeyListedTwice covers the same key written twice in one binding, which is
// either a typo or a key the writer meant to spell differently.
func TestLoadRejectsAKeyListedTwice(t *testing.T) {
	_, err := loadKeys(t, "[keybindings.nav]\ndown = [\"j\", \"j\"]\n")

	require.Error(t, err)
}

// TestLoadRejectsOneKeyOnTwoSettingsOfAGroup covers a key taken twice inside one group, which is
// where the loader can see the whole of it.
func TestLoadRejectsOneKeyOnTwoSettingsOfAGroup(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "movement up and down, which leaves no way back up the list",
			file: "[keybindings.nav]\ndown = [\"j\"]\nup = [\"j\"]\n",
		},
		{
			name: "the two filter matches",
			file: "[keybindings.filter]\nnext_match = [\"n\"]\nprev_match = [\"n\"]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadKeys(t, tt.file)

			require.Error(t, err, "one key cannot run two actions, so the file says one thing and the panel does another")
		})
	}
}

// TestLoadRejectsAnEmptyBinding covers a binding written as an empty list, which draws an action
// in the legend that no press can reach.
func TestLoadRejectsAnEmptyBinding(t *testing.T) {
	_, err := loadKeys(t, "[keybindings.nav]\ndown = []\n")

	require.Error(t, err, "a binding has to name a key, an action nothing reaches being worse than no action")
}

// TestLoadRejectsTheTopKeyOnTheBottomKey covers the doubled top press taking the key the bottom
// jump is on, where the first press latches and the jump it names never runs.
func TestLoadRejectsTheTopKeyOnTheBottomKey(t *testing.T) {
	_, err := loadKeys(t, "[keybindings.nav]\ntop = [\"G\"]\nbottom = [\"G\"]\n")

	require.Error(t, err)
}

// TestTheKeyTypeRangeCoversAllOfThem pins the span pressableNames reads.
func TestTheKeyTypeRangeCoversAllOfThem(t *testing.T) {
	assert.Empty(t, tea.KeyType(firstKeyType-1).String(), "a key type below the range has a name, so the range is too narrow")
	assert.Empty(t, tea.KeyType(lastKeyType+1).String(), "a key type above the range has a name, so the range is too narrow")
	assert.NotEmpty(t, pressableNames)
}

// TestIsPressableTakesEveryNameAPressProduces covers the accepting half of the name check.
func TestIsPressableTakesEveryNameAPressProduces(t *testing.T) {
	for _, key := range []string{
		"enter", "esc", "tab", "shift+tab", "backspace", "delete", "insert",
		"up", "down", "left", "right", "home", "end", "pgup", "pgdown",
		"f1", "f20", "ctrl+a", "ctrl+\\", "alt+j", "alt+ctrl+j", "shift+left", "ctrl+shift+home",
		" ", "a", "Z", "?", "/", "'", "<", "]", "\u00e9", "\u6f22",
	} {
		assert.True(t, newKeyChecker().isPressable(key), "bubbletea prints %q for a real press, so a config may name it", key)
	}
}

// TestIsPressableRefusesWhatNoPressProduces is the other half, including the modifier order
// bubbletea prints rather than the one a person would guess.
func TestIsPressableRefusesWhatNoPressProduces(t *testing.T) {
	for _, key := range []string{
		"", " j ", "aa", "escape", "return", "space", "PgUp", "Ctrl+A", "C-d", "<C-d>",
		"ctrl+m", "ctrl+i", "ctrl+shift+q", "f21", "ctrl+alt+j", "meta+j",
	} {
		assert.False(t, newKeyChecker().isPressable(key), "no press produces %q, so a binding on it is unreachable", key)
	}
}

// TestGetBoundKeysNamesEveryKeyOnce covers a key several settings share.
func TestGetBoundKeysNamesEveryKeyOnce(t *testing.T) {
	keys := DefaultKeybindings()
	keys.Nav.PageDown = []string{"ctrl+d", "pgdown"}
	keys.Column.ScrollDown = []string{"ctrl+d"}

	bound := keys.GetBoundKeys()

	assert.Equal(t, 1, countOf(bound, "ctrl+d"), "a key two settings share is named once")
	assert.Contains(t, bound, "pgdown")
	assert.IsIncreasing(t, bound)
	assert.NotEmpty(t, bound)
}

func countOf(keys []string, key string) int {
	count := 0
	for _, bound := range keys {
		if bound == key {
			count++
		}
	}

	return count
}
