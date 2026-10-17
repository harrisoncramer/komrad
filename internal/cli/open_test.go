//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/agents"

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
		DatabaseURL:         "postgres://komrad@db/komrad",
		ReadOnlyDatabaseURL: "postgres://komrad_readonly@db/komrad",
		Addr:                "127.0.0.1:9999",
		TmuxSocket:          "komrad-test",
		CacheDir:            "/tmp/cache",
		StateDir:            "/tmp/state",
		RunPostgres:         true,
		Editor:              "nvim",
	}}

	args := buildManagerSessionArgs(state, "/usr/local/bin/komrad", false)

	assert.Equal(t, map[string]string{
		"KOMRAD_DATABASE_URL":          "postgres://komrad@db/komrad",
		"KOMRAD_READONLY_DATABASE_URL": "postgres://komrad_readonly@db/komrad",
		"KOMRAD_ADDR":                  "127.0.0.1:9999",
		"KOMRAD_TMUX_SOCKET":           "komrad-test",
		"EDITOR":                       "nvim",
		"KOMRAD_CACHE_DIR":             "/tmp/cache",
		"KOMRAD_STATE_DIR":             "/tmp/state",
		"KOMRAD_RUN_POSTGRES":          "true",
	}, forwardedEnv(t, args))
}

// TestIntegrationBuildManagerSessionArgsCarriesTheConfigPath covers telling the panel which config
// file to read: the panel loads the file itself rather than receiving what this process parsed,
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
			want:  []string{"--", "/usr/local/bin/komrad", "panel"},
		},
		{
			name:  "a path given",
			stack: config.Stack{Paths: []string{"/tmp/somewhere/config.toml"}},
			want:  []string{"--", "/usr/local/bin/komrad", "panel", "--config", "/tmp/somewhere/config.toml"},
		},
		{
			name:  "an engine given",
			stack: config.Stack{Engine: "remote"},
			want:  []string{"--", "/usr/local/bin/komrad", "panel", "--engine", "remote"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &app{env: &config.Environment{}, configStack: tt.stack}

			args := buildManagerSessionArgs(state, "/usr/local/bin/komrad", false)

			separator := len(args) - len(tt.want)
			require.GreaterOrEqual(t, separator, 0, "the command line is shorter than what it has to end with")
			assert.Equal(t, tt.want, args[separator:])
		})
	}
}

// TestIntegrationBuildPanelWindowArgsOpensInTheSessionItAlreadyHas covers the repair path carrying everything
// the session path does.
func TestIntegrationBuildPanelWindowArgsOpensInTheSessionItAlreadyHas(t *testing.T) {
	state := &app{
		env: &config.Environment{
			DatabaseURL:         "postgres://komrad@db/komrad",
			ReadOnlyDatabaseURL: "postgres://komrad_readonly@db/komrad",
			Addr:                "127.0.0.1:9999",
			TmuxSocket:          "komrad-test",
			CacheDir:            "/tmp/cache",
			StateDir:            "/tmp/state",
			Editor:              "nvim",
		},
		configStack: config.Stack{Paths: []string{"/tmp/somewhere/config.toml"}},
	}

	args := buildPanelWindowArgs(state, "/usr/local/bin/komrad", false)

	assert.Equal(t, []string{"new-window", "-t", agents.ManagerSession + ":"}, args[:3])
	assert.Equal(t, forwardedEnv(t, buildManagerSessionArgs(state, "/usr/local/bin/komrad", false)), forwardedEnv(t, args))
	assert.Equal(t, []string{"--", "/usr/local/bin/komrad", "panel", "--config", "/tmp/somewhere/config.toml"},
		args[len(args)-5:])
}

// TestIntegrationIsPanelRunningNeedsThePanelsOwnPane covers the question attach asks before it
// decides there is a panel to attach.
func TestIntegrationIsPanelRunningNeedsThePanelsOwnPane(t *testing.T) {
	socket := throwawaySocket(t)
	panelSession(t, socket)

	state := panelState(t, socket)

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

			args := buildPanelWindowArgs(state, "/usr/local/bin/komrad", tt.detached)

			assert.Equal(t, tt.want, args[:len(tt.want)])
		})
	}
}

// TestIntegrationEnsureManagerSessionLeavesTheCurrentWindowAlone covers the repair not moving a watcher.
func TestIntegrationEnsureManagerSessionLeavesTheCurrentWindowAlone(t *testing.T) {
	socket := throwawaySocket(t)
	panelSession(t, socket)

	state := panelState(t, socket)

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
		DatabaseURL: "postgres://komrad@db/komrad",
		Addr:        "127.0.0.1:9999",
		TmuxSocket:  "komrad-test",
	}}

	args := buildManagerSessionArgs(state, "/usr/local/bin/komrad", true)

	assert.NotContains(t, args, "panel")
	assert.NotContains(t, args, "/usr/local/bin/komrad")
	assert.Contains(t, strings.Join(args, " "), "sleep")
	assert.Equal(t,
		forwardedEnv(t, buildManagerSessionArgs(state, "/usr/local/bin/komrad", false)),
		forwardedEnv(t, args),
		"a headless session still carries what an agent launched into it needs")
}

// sleepingPanel is a stand-in for the komrad binary whose panel subcommand only sleeps.
func sleepingPanel(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "komrad")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nsleep 60\n"), 0o755))

	return path
}

// TestIntegrationEnsureManagerSessionRepairsBesideAWindowNamedAfterTheSession covers the repair aiming at
// the session when a window in it carries the session's name.
func TestIntegrationEnsureManagerSessionRepairsBesideAWindowNamedAfterTheSession(t *testing.T) {
	socket := throwawaySocket(t)
	panelSession(t, socket)

	state := panelState(t, socket)
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

// TestIntegrationHostPathOutsideTmux covers a panel opened from a plain terminal, whose first workflow starts where komrad open ran.
func TestIntegrationHostPathOutsideTmux(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	want, err := os.Getwd()
	require.NoError(t, err)

	assert.Equal(t, want, hostPath(""))
}

// TestIntegrationRestartPanel covers a sign-out starting the panel over in its own window, without
// the terminal it showed beside it.
func TestIntegrationRestartPanel(t *testing.T) {
	type restartPanelCase struct {
		name      string
		hasPanel  bool
		panelURL  string
		wantPanes string
		wantSelf  bool
	}

	tests := []restartPanelCase{
		{
			name: "a machine with no panel open",
		},
		{
			name:      "a panel reaching another engine",
			hasPanel:  true,
			panelURL:  "https://another.example",
			wantPanes: "2",
		},
		{
			name:      "a panel on this engine showing an agent's terminal",
			hasPanel:  true,
			panelURL:  "https://engine.example",
			wantPanes: "1",
			wantSelf:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			socket := throwawaySocket(t)
			state := panelState(t, socket)
			state.self = sleepingPanel(t)
			state.env.URL = "https://engine.example"

			if tt.hasPanel {
				panelSession(t, socket)
				panelPane := hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{pane_id}")
				state.tmux.RecordPanelPane(panelPane)
				state.tmux.SetPanelURL(tt.panelURL)
				hostQuery(t, socket, "split-window", "-d", "-t", panelPane, "sleep 60")
			}

			require.NoError(t, restartPanel(t.Context(), state))

			if !tt.hasPanel {
				assert.False(t, isManagerSessionRunning(t.Context(), state))

				return
			}

			assert.Equal(t, tt.wantPanes, hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{window_panes}"))
			assert.Equal(t, tt.wantSelf, strings.Contains(hostQuery(t, socket, "display-message", "-p", "-t", agents.ManagerSession, "#{pane_start_command}"), state.self))
		})
	}
}
