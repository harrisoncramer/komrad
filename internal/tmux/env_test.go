//go:build integration

package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIntegrationFromEnv covers reading the user's server out of the TMUX variable tmux sets in the
// popup, where a value that is not the three fields tmux writes must yield nothing.
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
