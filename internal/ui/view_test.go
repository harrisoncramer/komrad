package ui_test

import (
	"errors"
	"slices"
	"testing"

	"komrade/internal/ui"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViewFatalScreens covers the screens that take the whole panel when it cannot start.
func TestViewFatalScreens(t *testing.T) {
	configErr := errors.New("failed to parse config /home/u/.config/komrade/config.toml: keybindings.list is not a setting")
	startupErr := errors.New("the agent server stopped during startup: failed to reach db: connection refused")

	tests := []struct {
		name        string
		configErr   error
		startupErr  error
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "a config error names the file to fix",
			configErr:   configErr,
			wantContain: []string{"Your configuration could not be read", "keybindings.list is not a setting", "config.toml"},
			wantAbsent:  []string{"Cannot reach the server"},
		},
		{
			name:        "a startup failure carries the reason the server gave",
			startupErr:  startupErr,
			wantContain: []string{"The agent server could not be started", "connection refused"},
			wantAbsent:  []string{"Cannot reach the server"},
		},
		{
			name:        "a config error outranks a startup failure, having happened first",
			configErr:   configErr,
			startupErr:  startupErr,
			wantContain: []string{"Your configuration could not be read"},
			wantAbsent:  []string{"The agent server could not be started"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{configErr: tt.configErr, startupErr: tt.startupErr})
			require.Nil(t, harness.panel.Init(), "nothing runs behind the screen")

			harness.watch(buildSnapshot(newLiveWorkflow("w1", "should-not-render")))
			harness.send(ui.NewWatchLost(errors.New("connection refused")))

			frame := joinProse(splitLines(harness.readFrame()))
			for _, want := range tt.wantContain {
				assert.Contains(t, frame, want)
			}
			for _, absent := range append(tt.wantAbsent, "should-not-render") {
				assert.NotContains(t, frame, absent)
			}

			assert.True(t, doesQuit(harness.panel.Update(ui.NewKeyPress("x"))), "any key dismisses the screen and quits")
		})
	}
}

func doesQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}

	switch msg := cmd().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		return slices.ContainsFunc(msg, doesQuit)
	default:
		return false
	}
}
