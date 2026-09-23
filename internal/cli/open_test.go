//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/version"

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

// TestIntegrationBuildManagerSessionArgsForwardsTheEnvironment covers which variables the manager
// session is created with; since a tmux server hands every session the environment it was
// itself started with, a variable left out here quietly keeps whatever value some long-gone
// shell had for it.
func TestIntegrationBuildManagerSessionArgsForwardsTheEnvironment(t *testing.T) {
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

// TestIntegrationBuildManagerSessionArgsCarriesTheConfigPath covers the panel being told which config
// file to read: it loads the file itself rather than being handed what this process parsed,
// so a path left off here is a panel reading the default while the caller named another.
func TestIntegrationBuildManagerSessionArgsCarriesTheConfigPath(t *testing.T) {
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

// TestIntegrationUserPanelKeyReadsTheServerTheUserIsAt covers the key the panel acts on coming off the
// user's own server.
func TestIntegrationUserPanelKeyReadsTheServerTheUserIsAt(t *testing.T) {
	managerSocket := throwawaySocket(t)
	panelSession(t, managerSocket)

	hostSocket := throwawaySocket(t)
	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "-f", "/dev/null",
		"new-session", "-d", "-s", "userland").Run())
	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "set-option", "-g", "@weave_key", "w").Run())

	hostPath := hostQuery(t, hostSocket, "display-message", "-p", "#{socket_path}")
	state := hideState(t, managerSocket, "")

	assert.Empty(t, userPanelKey(state, ""), "with no server to ask, the popup falls back to the built-in key")

	assert.Equal(t, "w", userPanelKey(state, hostPath), "the popup reads the server it opened on")

	state.tmux.RecordHostSocket(hostPath)

	assert.Equal(t, "w", userPanelKey(state, ""),
		"attaching from outside tmux still reads the server the panel was opened from")
}

// TestIntegrationBuildPanelWindowArgsOpensInTheSessionItAlreadyHas covers the repair path carrying everything
// the session path does.
func TestIntegrationBuildPanelWindowArgsOpensInTheSessionItAlreadyHas(t *testing.T) {
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

	assert.Equal(t, []string{"new-window", "-t", agents.ManagerSession + ":"}, args[:3])
	assert.Equal(t, forwardedEnv(t, buildManagerSessionArgs(state, "/usr/local/bin/weave", false)), forwardedEnv(t, args))
	assert.Equal(t, []string{"--", "/usr/local/bin/weave", "panel", "--config", "/tmp/somewhere/config.toml"},
		args[len(args)-5:])
}

// TestIntegrationIsPanelRunningNeedsThePanelsOwnPane covers the question the panel key asks before it
// decides there is a panel to attach.
func TestIntegrationIsPanelRunningNeedsThePanelsOwnPane(t *testing.T) {
	socket := throwawaySocket(t)
	panelSession(t, socket)

	state := hideState(t, socket, "")

	assert.False(t, isPanelRunning(t.Context(), state), "no panel has recorded a pane yet")

	panelPane := hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{pane_id}")
	state.tmux.RecordPanelPane(panelPane)

	assert.True(t, isPanelRunning(t.Context(), state))

	require.NoError(t, exec.Command("tmux", "-L", socket, "new-window", "-d", "-t", agents.ManagerSession).Run())
	require.NoError(t, exec.Command("tmux", "-L", socket, "kill-pane", "-t", panelPane).Run())

	assert.True(t, isManagerSessionRunning(t.Context(), state), "an agent's window keeps the session up")
	assert.False(t, isPanelRunning(t.Context(), state), "the panel's own pane has gone")
}

// TestIntegrationBuildPanelWindowArgsDetachesWhenNobodyIsAttaching covers a repair under --detach leaving the
// current window alone.
func TestIntegrationBuildPanelWindowArgsDetachesWhenNobodyIsAttaching(t *testing.T) {
	tests := []struct {
		name     string
		detached bool
		want     []string
	}{
		{
			name:     "about to attach",
			detached: false,
			want:     []string{"new-window", "-t", agents.ManagerSession + ":"},
		},
		{
			name:     "returning without attaching",
			detached: true,
			want:     []string{"new-window", "-d", "-t", agents.ManagerSession + ":"},
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

// TestIntegrationEnsureManagerSessionLeavesTheCurrentWindowAlone covers the repair not moving a watcher.
func TestIntegrationEnsureManagerSessionLeavesTheCurrentWindowAlone(t *testing.T) {
	socket := throwawaySocket(t)
	panelSession(t, socket)

	state := hideState(t, socket, "")

	panelPane := hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{pane_id}")
	state.tmux.RecordPanelPane(panelPane)

	require.NoError(t, exec.Command("tmux", "-L", socket, "new-window", "-t", agents.ManagerSession).Run())
	watching := hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{window_index}")

	require.NoError(t, ensureManagerSession(t.Context(), state, true, false))

	assert.Equal(t, watching, hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{window_index}"),
		"a panel that is already up is nothing to do, current window included")
}

// TestIntegrationBuildManagerSessionArgsHeadlessHoldsNoPanel covers an engine whose panel runs on another
// machine, where a panel here would swap an agent's pane into a stage nobody is looking at.
func TestIntegrationBuildManagerSessionArgsHeadlessHoldsNoPanel(t *testing.T) {
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

// TestIntegrationUserPanelKeyPrefersTheConfiguredKey covers the key the panel hides on being the one the
// binding was made with.
func TestIntegrationUserPanelKeyPrefersTheConfiguredKey(t *testing.T) {
	managerSocket := throwawaySocket(t)
	panelSession(t, managerSocket)

	hostSocket := throwawaySocket(t)
	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "-f", "/dev/null",
		"new-session", "-d", "-s", "userland").Run())

	hostPath := hostQuery(t, hostSocket, "display-message", "-p", "#{socket_path}")

	state := hideState(t, managerSocket, "")
	state.env.PanelKey = "w"

	assert.Equal(t, "w", userPanelKey(state, hostPath),
		"the config names the key the opener was bound on, so it names the one that hides too")

	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "set-option", "-g", "@weave_key", "j").Run())

	assert.Equal(t, "w", userPanelKey(state, hostPath),
		"the opener takes the config over the tmux option, so the hide key cannot take the other")

	state.env.PanelKey = ""

	assert.Equal(t, "j", userPanelKey(state, hostPath),
		"with no key configured the tmux option is still what both sides read")
}

// sleepingPanel is a stand-in for the weave binary whose panel subcommand only sleeps.
func sleepingPanel(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "weave")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nsleep 60\n"), 0o755))

	return path
}

// TestIntegrationEnsureManagerSessionReplacesAPanelFromAnotherBuild covers an open finding a panel built
// from other code, or one so old it never said which, and putting a fresh one up instead.
func TestIntegrationEnsureManagerSessionReplacesAPanelFromAnotherBuild(t *testing.T) {
	self := version.Describe(version.Version(), version.Commit())

	tests := []struct {
		name     string
		build    string
		recorded bool
		replaced bool
	}{
		{
			name:     "a panel that never recorded its build",
			build:    "",
			recorded: false,
			replaced: true,
		},
		{
			name:     "a panel from another build",
			build:    "devel (0000000)",
			recorded: true,
			replaced: true,
		},
		{
			name:     "a panel from this build",
			build:    self,
			recorded: true,
			replaced: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			socket := throwawaySocket(t)
			panelSession(t, socket)

			state := hideState(t, socket, "")
			state.self = sleepingPanel(t)

			panelPane := hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{pane_id}")
			state.tmux.RecordPanelPane(panelPane)

			if tt.recorded {
				state.tmux.RecordPanelBuild(tt.build)
			}

			before := hostQuery(t, socket, "display-message", "-p", "-t", panelPane, "#{pane_pid}")

			require.NoError(t, ensureManagerSession(t.Context(), state, true, false))

			after := strings.Fields(hostQuery(t, socket, "list-panes", "-a", "-F", "#{pane_pid}"))

			assert.Equal(t, !tt.replaced, slices.Contains(after, before),
				"the process the panel key found is the one it re-shows")
			assert.Len(t, after, 1, "one panel is up either way")
		})
	}
}

// TestIntegrationEnsureManagerSessionReplacesAPanelHoldingAGuest covers a stale panel whose window also
// holds the pane it was showing an agent in: an agent's own pane on a local engine, which has to
// survive in a window of its own, and a viewer on a remote engine, which goes with the window.
func TestIntegrationEnsureManagerSessionReplacesAPanelHoldingAGuest(t *testing.T) {
	tests := []struct {
		name     string
		owner    string
		survives bool
	}{
		{
			name:     "a viewer attached to a remote engine",
			owner:    "",
			survives: false,
		},
		{
			name:     "an agent's own pane on a local engine",
			owner:    "01a0c202-9943-70e2-b323-ab9a507b82bb",
			survives: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			socket := throwawaySocket(t)
			panelSession(t, socket)

			state := hideState(t, socket, "")
			state.self = sleepingPanel(t)

			panelPane := hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{pane_id}")
			state.tmux.RecordPanelPane(panelPane)
			state.tmux.RecordPanelBuild("devel (0000000)")
			require.NoError(t, exec.Command("tmux", "-L", socket, "rename-window", "-t", panelPane, "weave").Run())

			guest := hostQuery(t, socket, "split-window", "-d", "-h", "-t", panelPane, "-P", "-F", "#{pane_id}", "sleep 60")
			if tt.owner != "" {
				require.NoError(t, state.tmux.SetPaneOwner(guest, tt.owner))
			}

			panelPID := hostQuery(t, socket, "display-message", "-p", "-t", panelPane, "#{pane_pid}")
			guestPID := hostQuery(t, socket, "display-message", "-p", "-t", guest, "#{pane_pid}")

			require.NoError(t, ensureManagerSession(t.Context(), state, true, false))

			pids := strings.Fields(hostQuery(t, socket, "list-panes", "-a", "-F", "#{pane_pid}"))

			assert.NotContains(t, pids, panelPID, "the stale panel has gone")
			assert.Equal(t, tt.survives, slices.Contains(pids, guestPID))

			if tt.survives {
				assert.Len(t, pids, 2, "the fresh panel and the agent's pane")
				assert.Empty(t, state.tmux.GetSiblingPanes(guest), "the agent's pane stands in a window of its own")
			} else {
				assert.Len(t, pids, 1, "the fresh panel alone")
			}
		})
	}
}

// TestIntegrationEnsureManagerSessionRepairsBesideAWindowNamedAfterTheSession covers the repair aiming at
// the session when a window in it carries the session's name.
func TestIntegrationEnsureManagerSessionRepairsBesideAWindowNamedAfterTheSession(t *testing.T) {
	socket := throwawaySocket(t)
	panelSession(t, socket)

	state := hideState(t, socket, "")
	state.self = sleepingPanel(t)

	panelPane := hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{pane_id}")
	state.tmux.RecordPanelPane(panelPane)

	require.NoError(t, exec.Command("tmux", "-L", socket, "new-window", "-d", "-t", agents.ManagerSession,
		"-n", agents.ManagerSession, "sleep 60").Run())
	require.NoError(t, exec.Command("tmux", "-L", socket, "kill-pane", "-t", panelPane).Run())

	require.NoError(t, ensureManagerSession(t.Context(), state, true, false))

	assert.Len(t, strings.Fields(hostQuery(t, socket, "list-panes", "-a", "-F", "#{pane_id}")), 2,
		"the fresh panel beside the window that was there")
}
