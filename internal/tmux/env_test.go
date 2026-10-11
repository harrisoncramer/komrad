//go:build integration

package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIntegrationFromEnv covers reading the user's server out of the TMUX variable tmux sets in a
// pane, where a value that is not the three fields tmux writes must yield nothing.
func TestIntegrationFromEnv(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantSocket string
	}{
		{
			name:  "an unset variable names no server",
			value: "",
		},
		{
			name:  "a value short of the fields tmux writes is not read",
			value: "/tmp/tmux-501/default,74333",
		},
		{
			name:       "a value missing its session still names the server",
			value:      "/tmp/tmux-501/default,74333,",
			wantSocket: "/tmp/tmux-501/default",
		},
		{
			name:       "the socket comes first",
			value:      "/tmp/tmux-501/default,74333,0",
			wantSocket: "/tmp/tmux-501/default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantSocket, SocketFromEnv(tt.value))
		})
	}
}

// TestIntegrationDropLauncherEnv covers the environment a tmux client starts a server with, which
// must not carry the tmux client or mise task that launched komrad into every pane.
func TestIntegrationDropLauncherEnv(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		want    []string
	}{
		{
			name:    "the user's tmux server is dropped",
			environ: []string{"TMUX=/tmp/tmux-501/default,74333,0", "HOME=/home/me"},
			want:    []string{"HOME=/home/me"},
		},
		{
			name: "the launching mise task's roots and task are dropped",
			environ: []string{
				"MISE_CONFIG_ROOT=/home/me/komrad",
				"MISE_PROJECT_ROOT=/home/me/komrad",
				"MISE_ORIGINAL_CWD=/home/me/komrad",
				"MISE_TASK_NAME=restart",
				"MISE_TASK_FILE=/home/me/komrad/mise/tasks/restart",
				"MISE_TASK_PGID_MANAGED=1",
				"HOME=/home/me",
			},
			want: []string{"HOME=/home/me"},
		},
		{
			name: "what the panes need is kept",
			environ: []string{
				"MISE_ENV=test",
				"MISE_SHELL=zsh",
				"KOMRAD_CONFIG=/home/me/komrad/local/config.toml",
				"PATH=/usr/bin",
			},
			want: []string{
				"MISE_ENV=test",
				"MISE_SHELL=zsh",
				"KOMRAD_CONFIG=/home/me/komrad/local/config.toml",
				"PATH=/usr/bin",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, dropLauncherEnv(tt.environ))
		})
	}
}
