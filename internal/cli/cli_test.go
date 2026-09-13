//go:build integration

package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// TestIsConfigFatal covers which commands a broken config stops.
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

// TestResolveConfigPaths covers each way of naming a layer list replacing the one below
// rather than adding to it, which is what isolates a checkout from the shell.
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
		{
			name:     "the shell append idiom on an unset variable leaves a leading empty element",
			variable: ":/one.toml",
			want:     []string{"/one.toml"},
		},
		{
			name:     "a trailing separator leaves a dangling empty element",
			variable: "/one.toml:",
			want:     []string{"/one.toml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WEAVE_CONFIG", tt.variable)

			assert.Equal(t, tt.want, resolveConfigPaths(tt.flags))
		})
	}
}
