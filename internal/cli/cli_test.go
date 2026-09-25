//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"komrad/config"

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
			t.Setenv("KOMRAD_CONFIG", tt.variable)
			engines := filepath.Join(home, ".config", "komrad", "engines")
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
	t.Setenv("KOMRAD_CONFIG", "")

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

// TestIntegrationEveryCommandAnswersForItself walks the whole tree rather than the commands somebody wrote
// a test for.
func TestIntegrationEveryCommandAnswersForItself(t *testing.T) {
	walk(t, NewRootCommand(), func(t *testing.T, cmd *cobra.Command, path string) {
		assert.NotEmpty(t, cmd.Short, "%s says nothing about itself", path)
		assert.NotContains(t, cmd.Short, "\n", "%s runs its summary onto a second line", path)

		if !cmd.HasSubCommands() {
			assert.NotNil(t, cmd.Args, "%s takes any arguments it is given", path)
			assert.True(t, cmd.RunE != nil || cmd.Run != nil, "%s does nothing when run", path)
		}
	})
}

// TestIntegrationNoTwoCommandsAnswerToOneName pins the tree against a name bound twice, which cobra resolves
// to whichever was registered first and leaves the other unreachable.
func TestIntegrationNoTwoCommandsAnswerToOneName(t *testing.T) {
	walk(t, NewRootCommand(), func(t *testing.T, cmd *cobra.Command, path string) {
		held := map[string]string{}
		for _, child := range cmd.Commands() {
			for _, name := range append([]string{child.Name()}, child.Aliases...) {
				if was, taken := held[name]; taken && was != child.Name() {
					assert.Failf(t, "one name, two commands", "%s: %q is both %s and %s", path, name, was, child.Name())

					continue
				}
				held[name] = child.Name()
			}
		}
	})
}

func walk(t *testing.T, cmd *cobra.Command, check func(*testing.T, *cobra.Command, string)) {
	t.Helper()

	var descend func(*cobra.Command, string)
	descend = func(current *cobra.Command, path string) {
		check(t, current, path)

		for _, child := range current.Commands() {
			descend(child, strings.TrimSpace(path+" "+child.Name()))
		}
	}

	descend(cmd, cmd.Name())
}

// TestIntegrationQuietErrorsAreTheOnesAlreadyAnswered pins which failures main prints and which it
// swallows.
func TestIntegrationQuietErrorsAreTheOnesAlreadyAnswered(t *testing.T) {
	for _, quiet := range []error{ErrLogged, ErrReported, ErrPanelHidden} {
		assert.True(t, IsQuiet(quiet), "%v has already been answered", quiet)
	}

	assert.False(t, IsQuiet(nil))
	assert.False(t, IsQuiet(assert.AnError), "an ordinary failure is main's to print")
}
