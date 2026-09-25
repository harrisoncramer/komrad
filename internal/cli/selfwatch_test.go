package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecutableWatch covers what a rebuild does to the file the panel was started from.
func TestExecutableWatch(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, path string)
		want   bool
	}{
		{
			name:   "a file left alone",
			change: func(*testing.T, string) {},
			want:   false,
		},
		{
			name: "a file written over with different bytes",
			change: func(t *testing.T, path string) {
				t.Helper()
				require.NoError(t, os.WriteFile(path, []byte("new build, longer"), 0o755))
			},
			want: true,
		},
		{
			name: "a file written over with the same bytes at another time",
			change: func(t *testing.T, path string) {
				t.Helper()
				later := time.Now().Add(time.Hour)
				require.NoError(t, os.Chtimes(path, later, later))
			},
			want: true,
		},
		{
			name: "a file that has gone",
			change: func(t *testing.T, path string) {
				t.Helper()
				require.NoError(t, os.Remove(path))
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "komrad")
			require.NoError(t, os.WriteFile(path, []byte("old build"), 0o755))

			watch := watchFile(path)
			require.NotNil(t, watch)

			tt.change(t, path)

			assert.Equal(t, tt.want, watch.IsReplaced())
		})
	}
}

// TestWatchingAMissingFileWatchesNothing covers a binary that cannot be read at start.
func TestWatchingAMissingFileWatchesNothing(t *testing.T) {
	assert.Nil(t, watchFile(filepath.Join(t.TempDir(), "gone")))
}
