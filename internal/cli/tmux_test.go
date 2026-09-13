//go:build integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPanelKeySocket covers which tmux server the panel key is bound on, where inheriting it is
// the failure worth guarding.
func TestPanelKeySocket(t *testing.T) {
	const managerPath = "/tmp/tmux-501/weave"

	tests := []struct {
		name      string
		env       string
		host      string
		wantName  string
		wantPath  string
		wantErr   bool
		wantAsked int
	}{
		{
			name:     "outside tmux there is nothing to inherit",
			env:      "",
			wantName: "default",
		},
		{
			name:      "a pane on the user's own server binds there",
			env:       "/tmp/tmux-501/default,74333,0",
			wantPath:  "/tmp/tmux-501/default",
			wantAsked: 1,
		},
		{
			name:      "the popup binds on the server the panel recorded",
			env:       managerPath + ",8121,3",
			host:      "/tmp/tmux-501/default",
			wantPath:  "/tmp/tmux-501/default",
			wantAsked: 2,
		},
		{
			name:      "the popup with no recorded server refuses rather than binding on weave's own",
			env:       managerPath + ",8121,3",
			host:      "",
			wantErr:   true,
			wantAsked: 2,
		},
		{
			name:     "a TMUX value that is not the fields tmux writes names no server",
			env:      "nonsense",
			wantName: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asked := 0
			manager := func() string { asked++; return managerPath }
			host := func() string { asked++; return tt.host }

			socket, err := panelKeySocket(tt.env, manager, host)

			assert.Equal(t, tt.wantAsked, asked, "weave's own server was asked more than it had to be")

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
