package ui_test

import (
	"errors"
	"testing"

	"komrad/internal/ui/paneltest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViewFatalScreens covers the screens that take the whole panel when it cannot start.
func TestViewFatalScreens(t *testing.T) {
	configErr := errors.New("failed to parse config /home/u/.config/komrad/config.toml: keybindings.list is not a setting")
	startupErr := errors.New("the server stopped during startup: failed to reach db: connection refused")

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
			wantContain: []string{"The server could not be started", "connection refused"},
			wantAbsent:  []string{"Cannot reach the server"},
		},
		{
			name:        "a config error outranks a startup failure, having happened first",
			configErr:   configErr,
			startupErr:  startupErr,
			wantContain: []string{"Your configuration could not be read"},
			wantAbsent:  []string{"The server could not be started"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.Start(t, paneltest.Params{ConfigErr: tt.configErr, StartupErr: tt.startupErr})
			require.False(t, harness.IsWatching(), "nothing runs behind the screen")

			frame := paneltest.JoinProse(paneltest.SplitLines(harness.Frame()))
			for _, want := range tt.wantContain {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}

			assert.True(t, harness.Quits("x"), "any key dismisses the screen and quits")
		})
	}
}
