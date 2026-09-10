package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// loadKeys writes one config file and returns what Load made of it.
func loadKeys(t *testing.T, file string) (*Config, error) {
	t.Helper()

	clearConnectionEnvironment(t)

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(file), 0o600))

	return Load([]string{path})
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

// TestLoadRejectsAKeyTheHardCodedArmsHold covers binding an action to a key the panel answers
// itself, where the hard-coded arm wins wherever it is read.
func TestLoadRejectsAKeyTheHardCodedArmsHold(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "esc, which closes the filter, the details, the marks and the mail",
			file: "[keybindings.nav]\ndown = [\"esc\"]\n",
		},
		{
			name: "backspace, which the filter reads as a rubout",
			file: "[keybindings.nav]\ndown = [\"backspace\"]\n",
		},
		{
			name: "ctrl+g, which the message region takes to hand the cursor back",
			file: "[keybindings.details]\ncopy = [\"ctrl+g\"]\n",
		},
		{
			name: "tab, which the compose form reads as the next field",
			file: "[keybindings.review]\nresolve = [\"tab\"]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadKeys(t, tt.file)

			require.Error(t, err, "the panel answers this key itself, so the binding is unreachable where that arm is read")
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
