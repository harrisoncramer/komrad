//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"weave/config"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationIsConfigFatal covers which commands a broken config stops.
func TestIntegrationIsConfigFatal(t *testing.T) {
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

// TestIntegrationResolveConfigStack covers each way of naming a layer list replacing the one below rather than
// adding to it.
func TestIntegrationResolveConfigStack(t *testing.T) {
	tests := []struct {
		name     string
		flags    []string
		variable string
		engine   string
		want     config.Stack
	}{
		{
			name: "nothing named leaves the loader on its default path",
			want: config.Stack{},
		},
		{
			name:     "the variable is used when no flag was given",
			variable: "/one.toml:/two.toml",
			want:     config.Stack{Paths: []string{"/one.toml", "/two.toml"}},
		},
		{
			name:     "a flag replaces the variable rather than appending to it",
			flags:    []string{"/only.toml"},
			variable: "/one.toml:/two.toml",
			want:     config.Stack{Paths: []string{"/only.toml"}},
		},
		{
			name:     "the shell append idiom on an unset variable leaves a leading empty element",
			variable: ":/one.toml",
			want:     config.Stack{Paths: []string{"/one.toml"}},
		},
		{
			name:     "a trailing separator leaves a dangling empty element",
			variable: "/one.toml:",
			want:     config.Stack{Paths: []string{"/one.toml"}},
		},
		{
			name:   "an engine names no path of its own, so the default stays the default",
			engine: "remote",
			want:   config.Stack{Engine: "remote"},
		},
		{
			name:   "an engine lays over whatever the paths came to",
			flags:  []string{"/only.toml"},
			engine: "remote",
			want:   config.Stack{Paths: []string{"/only.toml"}, Engine: "remote"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("WEAVE_CONFIG", tt.variable)
			engines := filepath.Join(home, ".config", "weave", "engines")
			require.NoError(t, os.MkdirAll(engines, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(engines, "remote.toml"), []byte(""), 0o600))

			stack, err := resolveConfigStack(tt.flags, tt.engine)
			require.NoError(t, err)

			assert.Equal(t, tt.want, stack)
		})
	}
}

// TestIntegrationResolveConfigStackRefusesAnEngineItCannotFind covers a name no file answers to, which
// the local engine is a worse answer to than an error.
func TestIntegrationResolveConfigStackRefusesAnEngineItCannotFind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WEAVE_CONFIG", "")

	_, err := resolveConfigStack(nil, "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")

	_, err = resolveConfigStack(nil, "   ")
	require.Error(t, err, "a name made of spaces is a name, not a request for the local engine")
}

// TestIntegrationConfigArgs covers the flags a child process is given, which have to put it on the same
// files as the process that spawned it.
func TestIntegrationConfigArgs(t *testing.T) {
	assert.Empty(t, configArgs(config.Stack{}))
	assert.Equal(t, []string{"--engine", "remote"}, configArgs(config.Stack{Engine: "remote"}),
		"the default path is not spelled out, so the child leaves it optional the way the parent did")
	assert.Equal(t,
		[]string{"--config", "/one.toml", "--config", "/two.toml", "--engine", "remote"},
		configArgs(config.Stack{Paths: []string{"/one.toml", "/two.toml"}, Engine: "remote"}))
}
