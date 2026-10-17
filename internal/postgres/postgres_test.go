package postgres

import (
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsRunning covers a socket that answers and one that is not there.
func TestIsRunning(t *testing.T) {
	tests := []struct {
		name   string
		listen bool
		want   bool
	}{
		{
			name: "no socket",
		},
		{
			name:   "a listening socket",
			listen: true,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "komrad-pg") //nolint:usetesting // a socket path under the test directory runs past the length unix allows
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })

			if tt.listen {
				listener, err := net.Listen("unix", getSocketPath(dir))
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, listener.Close()) })
			}

			assert.Equal(t, tt.want, IsRunning(t.Context(), dir))
		})
	}
}
