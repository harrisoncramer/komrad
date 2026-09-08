package cli

import (
	"os/exec"
	"strings"
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func forwardedEnv(t *testing.T, args []string) map[string]string {
	t.Helper()

	forwarded := make(map[string]string)

	for i, arg := range args {
		if arg != "-e" {
			continue
		}

		require.Less(t, i+1, len(args), "-e is the last argument, so it names nothing")

		name, value, found := strings.Cut(args[i+1], "=")
		require.True(t, found, "a forwarded variable is written name=value")

		forwarded[name] = value
	}

	return forwarded
}

// TestBuildManagerSessionArgsForwardsTheEnvironment covers which variables the manager
// session is created with; since a tmux server hands every session the environment it was
// itself started with, a variable left out here quietly keeps whatever value some long-gone
// shell had for it.
func TestBuildManagerSessionArgsForwardsTheEnvironment(t *testing.T) {
	state := &app{env: &config.Environment{
		DatabaseURL:         "postgres://weave@db/weave",
		ReadOnlyDatabaseURL: "postgres://weave_readonly@db/weave",
		Addr:                "127.0.0.1:9999",
		TmuxSocket:          "weave-test",
		CacheDir:            "/tmp/cache",
		Editor:              "nvim",
	}}

	args := buildManagerSessionArgs(state, "/usr/local/bin/weave")

	assert.Equal(t, map[string]string{
		"WEAVE_DATABASE_URL":          "postgres://weave@db/weave",
		"WEAVE_READONLY_DATABASE_URL": "postgres://weave_readonly@db/weave",
		"WEAVE_ADDR":                  "127.0.0.1:9999",
		"WEAVE_TMUX_SOCKET":           "weave-test",
		"EDITOR":                      "nvim",
		"WEAVE_CACHE_DIR":             "/tmp/cache",
	}, forwardedEnv(t, args))
}

// TestBuildManagerSessionArgsCarriesTheConfigPath covers the panel being told which config
// file to read: it loads the file itself rather than being handed what this process parsed,
// so a path left off here is a panel reading the default while the caller named another.
func TestBuildManagerSessionArgsCarriesTheConfigPath(t *testing.T) {
	tests := []struct {
		name        string
		configPaths []string
		want        []string
	}{
		{
			name:        "no path given",
			configPaths: nil,
			want:        []string{"--", "/usr/local/bin/weave", "panel"},
		},
		{
			name:        "a path given",
			configPaths: []string{"/tmp/somewhere/config.toml"},
			want:        []string{"--", "/usr/local/bin/weave", "panel", "--config", "/tmp/somewhere/config.toml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &app{env: &config.Environment{}, configPaths: tt.configPaths}

			args := buildManagerSessionArgs(state, "/usr/local/bin/weave")

			separator := len(args) - len(tt.want)
			require.GreaterOrEqual(t, separator, 0, "the command line is shorter than what it has to end with")
			assert.Equal(t, tt.want, args[separator:])
		})
	}
}

// TestUserPanelKeyReadsTheServerTheUserIsAt covers the key the panel acts on coming off the
// user's own server.
func TestUserPanelKeyReadsTheServerTheUserIsAt(t *testing.T) {
	managerSocket := throwawaySocket(t)
	panelSession(t, managerSocket)

	hostSocket := throwawaySocket(t)
	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "-f", "/dev/null",
		"new-session", "-d", "-s", "userland").Run())
	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "set-option", "-g", "@weave_key", "w").Run())

	hostPath := hostQuery(t, hostSocket, "display-message", "-p", "#{socket_path}")
	state := hideState(managerSocket, "")

	assert.Empty(t, userPanelKey(state, ""), "with no server to ask, the popup falls back to the built-in key")

	assert.Equal(t, "w", userPanelKey(state, hostPath), "the popup reads the server it opened on")

	state.tmux.RecordHostSocket(hostPath)

	assert.Equal(t, "w", userPanelKey(state, ""),
		"attaching from outside tmux still reads the server the panel was opened from")
}
