package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequireVersion covers which versions a CLI prints meet the floor komrad launches it at.
func TestRequireVersion(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr bool
	}{
		{
			name:    "an older patch",
			output:  "2.1.223 (Claude Code)",
			wantErr: true,
		},
		{
			name:    "an older minor whose patch is higher",
			output:  "2.0.999 (Claude Code)",
			wantErr: true,
		},
		{
			name:    "output naming no version",
			output:  "command not recognised",
			wantErr: true,
		},
		{
			name:   "the floor itself",
			output: "2.1.224 (Claude Code)",
		},
		{
			name:   "a newer version whose part is wider than the floor's",
			output: "codex-cli 2.1.1000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireVersion("claude", tt.output, "2.1.224")
			if !tt.wantErr {
				require.NoError(t, err)

				return
			}

			assert.ErrorIs(t, err, errUnsupportedVersion)
		})
	}
}
