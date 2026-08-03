package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// TestIsConfigFatal covers which commands a config that will not parse stops. It matters
// because the file names the database now: a command that carried on would fall back to the
// built-in default and write to a database the user did not mean, quite possibly a stale local
// one, with only a log line to say so.
//
// The three that survive each have a reason. doctor is what a broken config is diagnosed with.
// version answers about the build and touches nothing. panel would write its error into a
// popup tmux destroys in the same instant, so it draws a screen instead.
func TestIsConfigFatal(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{
			name:    "serve, which would otherwise open the wrong database",
			command: "serve",
			want:    true,
		},
		{
			name:    "open, which spawns the service",
			command: "open",
			want:    true,
		},
		{
			name:    "start, which spawns the service",
			command: "start",
			want:    true,
		},
		{
			name:    "migrate, which writes schema",
			command: "migrate",
			want:    true,
		},
		{
			name:    "readonly-role, which grants on a database",
			command: "readonly-role",
			want:    true,
		},
		{
			name:    "doctor, which is the command this is diagnosed with",
			command: "doctor",
			want:    false,
		},
		{
			name:    "version, which needs no config at all",
			command: "version",
			want:    false,
		},
		{
			name:    "panel, which shows the error on a screen instead",
			command: "panel",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConfigFatal(&cobra.Command{Use: tt.command})

			assert.Equal(t, tt.want, got)
		})
	}
}

// TestResolveConfigPaths covers the three ways a layer list is named. Each replaces the one
// below it rather than adding to it, since a checkout that could not drop a layer the shell
// exported would not be isolated from it, and that isolation is what mise.test.toml is for.
func TestResolveConfigPaths(t *testing.T) {
	tests := []struct {
		name     string
		flags    []string
		variable string
		want     []string
	}{
		{
			name: "nothing named leaves the loader on its default path",
			want: nil,
		},
		{
			name:     "the variable is used when no flag was given",
			variable: "/one.toml:/two.toml",
			want:     []string{"/one.toml", "/two.toml"},
		},
		{
			name:     "a flag replaces the variable rather than appending to it",
			flags:    []string{"/only.toml"},
			variable: "/one.toml:/two.toml",
			want:     []string{"/only.toml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WEAVE_CONFIG", tt.variable)

			assert.Equal(t, tt.want, resolveConfigPaths(tt.flags))
		})
	}
}
