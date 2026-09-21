//go:build integration

package cli

import (
	"testing"

	"weave/internal/tmux"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPanelKeySocket covers which tmux server the panel key is bound on, where inheriting it is
// the failure worth guarding.
func TestPanelKeySocket(t *testing.T) {
	const managerPath = "/tmp/tmux-501/weave"

	tests := []struct {
		name     string
		env      string
		host     string
		wantName string
		wantPath string
		wantErr  bool
	}{
		{
			name:     "outside tmux there is nothing to inherit",
			env:      "",
			wantName: "default",
		},
		{
			name:     "a pane on the user's own server binds there",
			env:      "/tmp/tmux-501/default,74333,0",
			wantPath: "/tmp/tmux-501/default",
		},
		{
			name:     "the popup binds on the server the panel recorded",
			env:      managerPath + ",8121,3",
			host:     "/tmp/tmux-501/default",
			wantPath: "/tmp/tmux-501/default",
		},
		{
			name:     "a pane on another engine's server binds on the server that engine recorded",
			env:      "/tmp/tmux-501/weave-remote,8121,3",
			host:     "/tmp/tmux-501/default",
			wantPath: "/tmp/tmux-501/default",
		},
		{
			name:    "the popup with no recorded server refuses rather than binding on weave's own",
			env:     managerPath + ",8121,3",
			host:    "",
			wantErr: true,
		},
		{
			name:     "a TMUX value that is not the fields tmux writes names no server",
			env:      "nonsense",
			wantName: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := func() string { return managerPath }
			hostOf := func(socket string) string {
				require.Equal(t, tmux.SocketFromEnv(tt.env), socket, "the server asked is the one the pane is on")

				return tt.host
			}

			socket, err := panelKeySocket(tt.env, manager, hostOf)

			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, socket.name)
			assert.Equal(t, tt.wantPath, socket.path)
		})
	}
}
