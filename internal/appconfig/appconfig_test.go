package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFillKeys(t *testing.T) {
	tests := []struct {
		name string
		val  []string
		def  []string
		want []string
	}{
		{
			name: "an empty value falls back to the default",
			val:  nil,
			def:  []string{"a", "b"},
			want: []string{"a", "b"},
		},
		{
			name: "a set value is kept",
			val:  []string{"x"},
			def:  []string{"a"},
			want: []string{"x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, fillKeys(tt.val, tt.def))
		})
	}
}

func TestWithDefaults(t *testing.T) {
	t.Run("an empty config becomes the full defaults", func(t *testing.T) {
		assert.Equal(t, DefaultKeybindings(), Keybindings{}.withDefaults())
	})

	t.Run("a set binding is kept while the rest fill in", func(t *testing.T) {
		partial := Keybindings{Nav: NavKeys{Up: []string{"w"}}}

		got := partial.withDefaults()
		assert.Equal(t, []string{"w"}, got.Nav.Up, "the overridden binding is kept")
		assert.Equal(t, DefaultKeybindings().Nav.Down, got.Nav.Down, "an unset binding falls back to default")
		assert.Equal(t, DefaultKeybindings().Chats.Open, got.Chats.Open, "an unset panel falls back to default")
	})
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.ClaudeFlags)
	assert.Equal(t, DefaultKeybindings(), cfg.Keybindings)
}

func TestLoadMergesFileOverDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
claude_flags = "--model opus"

[keybindings.nav]
up = ["w"]
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "--model opus", cfg.ClaudeFlags)
	assert.Equal(t, []string{"w"}, cfg.Keybindings.Nav.Up, "the file override wins")
	assert.Equal(t, DefaultKeybindings().Nav.Down, cfg.Keybindings.Nav.Down, "unset bindings still default")
}
