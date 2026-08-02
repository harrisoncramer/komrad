package tmux

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientTarget(t *testing.T) {
	tests := []struct {
		name   string
		client string
		want   []string
	}{
		{
			name:   "an empty client yields no target flags",
			client: "",
			want:   nil,
		},
		{
			name:   "a named client yields a -c target",
			client: "/dev/ttys001",
			want:   []string{"-c", "/dev/ttys001"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clientTarget(tt.client))
		})
	}
}

// TestQueryCarriesStderr covers what a failed tmux call reports. These run on a loop with
// nobody watching, so an exit status with neither the command nor tmux's own complaint in
// it is the difference between a diagnosable failure and a mystery.
func TestQueryCarriesStderr(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}

	_, err := NewClient("weave-test-no-such-socket").GetPaneOwners()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list-panes", "the failing command is named")
	assert.Contains(t, err.Error(), "weave-test-no-such-socket", "so is the server it ran against")
	assert.Contains(t, err.Error(), "error connecting", "and tmux's own stderr comes through")
}
