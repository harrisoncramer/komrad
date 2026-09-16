//go:build integration

package cli

import (
	"os/exec"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/agents"

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

	args := buildManagerSessionArgs(state, "/usr/local/bin/weave", false)

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
		name  string
		stack config.Stack
		want  []string
	}{
		{
			name:  "no path given",
			stack: config.Stack{},
			want:  []string{"--", "/usr/local/bin/weave", "panel"},
		},
		{
			name:  "a path given",
			stack: config.Stack{Paths: []string{"/tmp/somewhere/config.toml"}},
			want:  []string{"--", "/usr/local/bin/weave", "panel", "--config", "/tmp/somewhere/config.toml"},
		},
		{
			name:  "an engine given",
			stack: config.Stack{Engine: "remote"},
			want:  []string{"--", "/usr/local/bin/weave", "panel", "--engine", "remote"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &app{env: &config.Environment{}, configStack: tt.stack}

			args := buildManagerSessionArgs(state, "/usr/local/bin/weave", false)

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

// TestBuildPanelWindowArgsOpensInTheSessionItAlreadyHas covers the repair path carrying
// everything the session path does, since it opens the same process on the same settings.
func TestBuildPanelWindowArgsOpensInTheSessionItAlreadyHas(t *testing.T) {
	state := &app{
		env: &config.Environment{
			DatabaseURL:         "postgres://weave@db/weave",
			ReadOnlyDatabaseURL: "postgres://weave_readonly@db/weave",
			Addr:                "127.0.0.1:9999",
			TmuxSocket:          "weave-test",
			CacheDir:            "/tmp/cache",
			Editor:              "nvim",
		},
		configStack: config.Stack{Paths: []string{"/tmp/somewhere/config.toml"}},
	}

	args := buildPanelWindowArgs(state, "/usr/local/bin/weave", false)

	assert.Equal(t, []string{"new-window", "-t", agents.ManagerSession}, args[:3])
	assert.Equal(t, forwardedEnv(t, buildManagerSessionArgs(state, "/usr/local/bin/weave", false)), forwardedEnv(t, args))
	assert.Equal(t, []string{"--", "/usr/local/bin/weave", "panel", "--config", "/tmp/somewhere/config.toml"},
		args[len(args)-5:])
}

// Explainer: the manager session holds a window per agent as well as the panel's own, so it
// answers has-session for as long as any agent is up. Reading that answer alone reports a panel
// that has been gone for hours, and the key then attaches a session with no panel in it.

// TestIsPanelRunningNeedsThePanelsOwnPane covers the question the panel key asks before it
// decides there is a panel to attach.
func TestIsPanelRunningNeedsThePanelsOwnPane(t *testing.T) {
	socket := throwawaySocket(t)
	panelSession(t, socket)

	state := hideState(socket, "")

	assert.False(t, isPanelRunning(t.Context(), state), "no panel has recorded a pane yet")

	panelPane := hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{pane_id}")
	state.tmux.RecordPanelPane(panelPane)

	assert.True(t, isPanelRunning(t.Context(), state))

	require.NoError(t, exec.Command("tmux", "-L", socket, "new-window", "-d", "-t", agents.ManagerSession).Run())
	require.NoError(t, exec.Command("tmux", "-L", socket, "kill-pane", "-t", panelPane).Run())

	assert.True(t, isManagerSessionRunning(t.Context(), state), "an agent's window keeps the session up")
	assert.False(t, isPanelRunning(t.Context(), state), "the panel's own pane has gone")
}

// TestBuildPanelWindowArgsDetachesWhenNobodyIsAttaching covers a repair under --detach leaving
// the current window alone, since a script that only wants the session there would otherwise
// move whoever is watching one off the agent they were reading.
func TestBuildPanelWindowArgsDetachesWhenNobodyIsAttaching(t *testing.T) {
	tests := []struct {
		name     string
		detached bool
		want     []string
	}{
		{
			name:     "about to attach",
			detached: false,
			want:     []string{"new-window", "-t", agents.ManagerSession},
		},
		{
			name:     "returning without attaching",
			detached: true,
			want:     []string{"new-window", "-d", "-t", agents.ManagerSession},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &app{env: &config.Environment{}}

			args := buildPanelWindowArgs(state, "/usr/local/bin/weave", tt.detached)

			assert.Equal(t, tt.want, args[:len(tt.want)])
		})
	}
}

// Explainer: select-window moves the current window for every client attached to the session, so
// only the path that is about to attach may touch it. ensureManagerSession runs under --detach
// too, which is what mise/tasks/restart uses, and a script asking for a session is not asking to
// be taken anywhere.

// TestEnsureManagerSessionLeavesTheCurrentWindowAlone covers the repair not moving a watcher.
func TestEnsureManagerSessionLeavesTheCurrentWindowAlone(t *testing.T) {
	socket := throwawaySocket(t)
	panelSession(t, socket)

	state := hideState(socket, "")

	panelPane := hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{pane_id}")
	state.tmux.RecordPanelPane(panelPane)

	require.NoError(t, exec.Command("tmux", "-L", socket, "new-window", "-t", agents.ManagerSession).Run())
	watching := hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{window_index}")

	require.NoError(t, ensureManagerSession(t.Context(), state, true, false))

	assert.Equal(t, watching, hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{window_index}"),
		"a panel that is already up is nothing to do, current window included")
}

// TestBuildManagerSessionArgsHeadlessHoldsNoPanel covers an engine whose panel runs on another
// machine, where a panel here would swap an agent's pane into a stage nobody is looking at.
func TestBuildManagerSessionArgsHeadlessHoldsNoPanel(t *testing.T) {
	state := &app{env: &config.Environment{
		DatabaseURL: "postgres://weave@db/weave",
		Addr:        "127.0.0.1:9999",
		TmuxSocket:  "weave-test",
	}}

	args := buildManagerSessionArgs(state, "/usr/local/bin/weave", true)

	assert.NotContains(t, args, "panel")
	assert.NotContains(t, args, "/usr/local/bin/weave")
	assert.Contains(t, strings.Join(args, " "), "sleep")
	assert.Equal(t,
		forwardedEnv(t, buildManagerSessionArgs(state, "/usr/local/bin/weave", false)),
		forwardedEnv(t, args),
		"a headless session still carries what an agent launched into it needs")
}
