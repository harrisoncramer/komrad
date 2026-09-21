//go:build integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/doctor"
	"weave/internal/harness"
	"weave/internal/harness/claude"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDoctorReadsTheConfigTheUserNamed covers which path the config check is made against:
// the resolved path is always a file name, so reading it cannot tell a config nobody wrote
// apart from one somebody named and never created.
func TestDoctorReadsTheConfigTheUserNamed(t *testing.T) {
	tests := []struct {
		name    string
		stack   config.Stack
		wantErr string
	}{
		{
			name:    "a file named on the command line that is not there",
			stack:   config.Stack{Paths: []string{"/no/such/config.toml"}},
			wantErr: "/no/such/config.toml",
		},
		{
			name:  "no file and none named, which is every fresh install",
			stack: config.Stack{},
		},
		{
			name:  "an engine on a machine that never wrote a config file",
			stack: config.Stack{Engine: "remote"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			engines := filepath.Join(home, ".config", "weave", "engines")
			require.NoError(t, os.MkdirAll(engines, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(engines, "remote.toml"), []byte(""), 0o600))

			registry, err := harness.NewRegistry(claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}))
			require.NoError(t, err)

			state := &app{
				env:         &config.Environment{},
				harnesses:   registry,
				configStack: tt.stack,
			}

			env, _, err := state.buildDoctor()
			require.NoError(t, err)

			_, err = env.LoadConfig()

			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr, "the error names the file that is not there")
		})
	}
}

// TestDoctorSurvivesAnEmptyLayerElement covers a layer list that will not resolve still
// reaching a report, which an unset WEAVE_CONFIG produces through the append idiom.
func TestDoctorSurvivesAnEmptyLayerElement(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry, err := harness.NewRegistry(claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}))
	require.NoError(t, err)

	state := &app{
		env:         &config.Environment{},
		harnesses:   registry,
		configStack: config.Stack{Paths: []string{"", "/no/such/config.toml"}},
	}

	env, _, err := state.buildDoctor()
	require.NoError(t, err, "doctor has to reach a report rather than aborting before any check runs")

	_, loadErr := env.LoadConfig()
	require.Error(t, loadErr, "the config check has to be the one that reports the broken layer list")
	assert.Contains(t, loadErr.Error(), "empty path")
}

// TestDoctorAdviceReadsThePanelsOwnPane covers the doctor asking the same question the panel
// key asks.
func TestDoctorAdviceReadsThePanelsOwnPane(t *testing.T) {
	socket := throwawaySocket(t)
	panelSession(t, socket)

	registry, err := harness.NewRegistry(claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}))
	require.NoError(t, err)

	state := hideState(t, socket, "")
	state.env.Addr = "127.0.0.1:8787"
	state.harnesses = registry

	staleFix := func(t *testing.T) string {
		t.Helper()

		env, settings, err := state.buildDoctor()
		require.NoError(t, err)
		checks := doctor.Checks(settings)

		env.ProbeServer = func(context.Context) (doctor.ServerStatus, error) {
			return doctor.ServerStatus{
				Answering: true,
				Ready:     true,
				Version:   "v1.0.0",
			}, nil
		}

		index := slices.IndexFunc(checks, func(check doctor.Check) bool { return check.Name == "server" })
		require.GreaterOrEqual(t, index, 0)

		return checks[index].Run(t.Context(), env).Fix
	}

	assert.Equal(t, "weave stop, then reopen the panel", staleFix(t),
		"an agent's window holds the session up with no panel drawing")

	state.tmux.RecordPanelPane(hostQuery(t, socket, "display-message", "-p",
		"-t", agents.ManagerSession, "#{pane_id}"))

	assert.Contains(t, staleFix(t), "kill-server",
		"a panel is drawing, so stopping the server under it would strand it")
}
