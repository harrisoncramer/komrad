package xfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteAtomic pins that a write replaces the file whole and leaves nothing staged beside it.
func TestWriteAtomic(t *testing.T) {
	tests := []struct {
		name     string
		existing []byte
		content  []byte
		mode     os.FileMode
		wantErr  bool
	}{
		{
			name:    "a directory that does not exist",
			content: []byte("new"),
			mode:    0o600,
			wantErr: true,
		},
		{
			name:    "a file that does not exist yet",
			content: []byte("new"),
			mode:    0o600,
		},
		{
			name:     "a file that is already there",
			existing: []byte("old contents that run longer"),
			content:  []byte("new"),
			mode:     0o755,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file")
			if tt.wantErr {
				path = filepath.Join(dir, "missing", "file")
			}
			if tt.existing != nil {
				require.NoError(t, os.WriteFile(path, tt.existing, 0o644))
			}

			err := WriteAtomic(path, tt.content, tt.mode)
			if tt.wantErr {
				require.Error(t, err)

				return
			}
			require.NoError(t, err)

			written, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.content, written)

			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, tt.mode, info.Mode().Perm())

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Len(t, entries, 1)
		})
	}
}
