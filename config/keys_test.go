package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
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

// TestLoadRejectsOneKeyOnTwoActions covers a key taken by two bindings that are both live on
// the same screen, where precedence silently decides which one is reachable.
func TestLoadRejectsOneKeyOnTwoActions(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "movement and a screen action, movement winning and the action never running",
			file: "[keybindings.agents]\nnew = [\"j\"]\n",
		},
		{
			name: "two bindings inside one group",
			file: "[keybindings.nav]\ndown = [\"j\"]\nup = [\"j\"]\n",
		},
		{
			name: "the help toggle taking the quit key, which leaves the screen with no quit",
			file: "[keybindings.help]\ntoggle = [\"ctrl+c\"]\n",
		},
		{
			name: "the filter and the details focus on one key",
			file: "[keybindings.filter]\nopen = [\"ctrl+t\"]\n",
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

// TestLoadKeepsTheDefaultForAnEmptyList covers a binding written as an empty list, which today
// unbinds the action silently while the documented behaviour is a fallback to the default.
func TestLoadKeepsTheDefaultForAnEmptyList(t *testing.T) {
	cfg, err := loadKeys(t, "[keybindings.nav]\ndown = []\n")

	require.NoError(t, err)
	assert.Equal(t, DefaultKeybindings().Nav.Down, cfg.Keybindings.Nav.Down,
		"an empty list reads as the key being absent, which the doc comment says takes the default")
}

// TestLoadRejectsAFocusKeyTmuxCannotBind covers focus_key, which is a tmux key name rather than
// a bubbletea one and is bound with the error swallowed.
func TestLoadRejectsAFocusKeyTmuxCannotBind(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "the bubbletea spelling of a control key, which tmux calls an unknown key",
			file: "[panel]\nfocus_key = \"ctrl+a\"\n",
		},
		{
			name: "a name tmux does not know at all",
			file: "[panel]\nfocus_key = \"nope\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadKeys(t, tt.file)

			require.Error(t, err, "tmux refuses to bind this, and the refusal is only a warning in the log")
		})
	}
}

// TestLoadRejectsAPrintableFocusKey covers focus_key set to a key an agent types, which is bound
// outside the prefix table and so is taken from every pane on weave's tmux server.
func TestLoadRejectsAPrintableFocusKey(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "a letter, which the agent's own CLI never receives again",
			file: "[panel]\nfocus_key = \"j\"\n",
		},
		{
			name: "a digit",
			file: "[panel]\nfocus_key = \"1\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadKeys(t, tt.file)

			require.Error(t, err, "a root binding on a printable key takes it from the panel and from every agent")
		})
	}
}
