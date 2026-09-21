package xpath

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsSameDir pins that a path reaches the same directory through a symlink, a trailing
// slash, or neither, and that two directories are still two.
func TestIsSameDir(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	require.NoError(t, os.Mkdir(target, 0o755))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(target, link))
	other := filepath.Join(root, "other")
	require.NoError(t, os.Mkdir(other, 0o755))

	assert.True(t, IsSameDir(target, target))
	assert.True(t, IsSameDir(target, target+string(filepath.Separator)))
	assert.True(t, IsSameDir(link, target))
	assert.False(t, IsSameDir(target, other))
	assert.False(t, IsSameDir(filepath.Join(root, "missing"), target))
}
