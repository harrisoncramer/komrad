package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteGenerated(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, WriteGenerated(dir, "_worktree_setup_a", "#!/bin/sh\necho one\n"))

	path := filepath.Join(dir, "_worktree_setup_a")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o111, "the written file is executable")

	require.NoError(t, WriteGenerated(dir, "_worktree_setup_a", "#!/bin/sh\necho two\n"))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\necho two\n", string(content), "a second write overwrites the first")
}

func TestWriteGeneratedMakesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "actions")

	require.NoError(t, WriteGenerated(dir, "_worktree_setup_a", "#!/bin/sh\n"))

	_, err := os.Stat(filepath.Join(dir, "_worktree_setup_a"))
	assert.NoError(t, err)
}

func TestRemoveGenerated(t *testing.T) {
	tests := []struct {
		name   string
		create bool
	}{
		{
			name: "removing one that was never written is not an error",
		},
		{
			name:   "removing one that exists deletes it",
			create: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			if tt.create {
				require.NoError(t, WriteGenerated(dir, "_worktree_cleanup_a", "#!/bin/sh\n"))
			}

			require.NoError(t, RemoveGenerated(dir, "_worktree_cleanup_a"))

			_, err := os.Stat(filepath.Join(dir, "_worktree_cleanup_a"))
			assert.True(t, os.IsNotExist(err))
		})
	}
}
