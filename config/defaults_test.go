package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContextFile covers the default context path being filled in at read time, since one
// machine's home directory would be wrong on every other machine.
func TestContextFile(t *testing.T) {
	tests := []struct {
		name string
		file string
		// want is the exact path when the config names one, and wantSuffix is what the
		// filled-in default has to end in.
		want       string
		wantSuffix string
	}{
		{
			name:       "no config at all falls back beside the config file",
			file:       "",
			wantSuffix: filepath.Join(".config", "weave", "context.md"),
		},
		{
			name:       "a blank entry is the same as naming none",
			file:       "[context]\nfile = \"   \"\n",
			wantSuffix: filepath.Join(".config", "weave", "context.md"),
		},
		{
			name: "a file the config names is taken as given",
			file: "[context]\nfile = \"/work/notes/standing.md\"\n",
			want: "/work/notes/standing.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := Load([]string{path})
			require.NoError(t, err)

			got, err := cfg.GetContextFile()
			require.NoError(t, err)

			if tt.want != "" {
				assert.Equal(t, tt.want, got)

				return
			}

			assert.True(t, strings.HasSuffix(got, tt.wantSuffix), "got %q", got)
		})
	}
}

// TestConfiguredPathsExpandHome covers the ~ a user writes a path with, which nothing below
// expands and which silently does nothing when it is left as it was typed.
func TestConfiguredPathsExpandHome(t *testing.T) {
	tests := []struct {
		name string
		file string
		// want is what the getter has to return, given the home the test set.
		want func(home string) string
		read func(Config) (string, error)
	}{
		{
			name: "a path that only begins with a tilde is not a home path",
			file: "[context]\nfile = \"~notahome/context.md\"\n",
			want: func(string) string { return "~notahome/context.md" },
			read: Config.GetContextFile,
		},
		{
			name: "an actions directory under ~ resolves to the home directory",
			file: "[actions]\ndir = \"~/dotfiles/weave/actions\"\n",
			want: func(home string) string { return filepath.Join(home, "dotfiles", "weave", "actions") },
			read: Config.GetActionsDir,
		},
		{
			name: "a bare ~ is the home directory itself",
			file: "[actions]\ndir = \"~\"\n",
			want: func(home string) string { return home },
			read: Config.GetActionsDir,
		},
		{
			name: "a context file under ~ resolves to the home directory",
			file: "[context]\nfile = \"~/dotfiles/weave/context.md\"\n",
			want: func(home string) string { return filepath.Join(home, "dotfiles", "weave", "context.md") },
			read: Config.GetContextFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := Load([]string{path})
			require.NoError(t, err)

			got, err := tt.read(*cfg)
			require.NoError(t, err)

			assert.Equal(t, tt.want(home), got)
		})
	}
}
