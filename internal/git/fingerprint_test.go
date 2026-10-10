//go:build integration

package git

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationReadFingerprint covers which changes to a worktree a fingerprint sees, including
// an edit to a file git status is told not to read.
func TestIntegrationReadFingerprint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func(t *testing.T, root string)
		change   func(t *testing.T, root string)
		wantSame bool
	}{
		{
			name:     "an untouched worktree fingerprints alike",
			setup:    func(*testing.T, string) {},
			change:   func(*testing.T, string) {},
			wantSame: true,
		},
		{
			name:  "an edit to a clean file is seen",
			setup: func(*testing.T, string) {},
			change: func(t *testing.T, root string) {
				write(t, root, "kept.go", "package main\n\nvar edited = true\n")
			},
		},
		{
			name: "a second edit to a modified file is seen",
			setup: func(t *testing.T, root string) {
				write(t, root, "kept.go", "package main\n\nvar edited = true\n")
			},
			change: func(t *testing.T, root string) {
				write(t, root, "kept.go", "package main\n\nvar editedAgain = true\n")
			},
		},
		{
			name: "an edit to an untracked file is seen",
			setup: func(t *testing.T, root string) {
				write(t, root, "scratch.txt", "one\n")
			},
			change: func(t *testing.T, root string) {
				write(t, root, "scratch.txt", "one and two\n")
			},
		},
		{
			name: "an edit to a skip-worktree file is seen",
			setup: func(t *testing.T, root string) {
				run(t, root, "update-index", "--skip-worktree", "kept.go")
			},
			change: func(t *testing.T, root string) {
				write(t, root, "kept.go", "package main\n\nvar hidden = true\n")
			},
		},
		{
			name: "an edit to an ignored file is not",
			setup: func(t *testing.T, root string) {
				write(t, root, ".gitignore", "ignored.txt\n")
				run(t, root, "add", ".gitignore")
				run(t, root, "commit", "-m", "ignore")
				write(t, root, "ignored.txt", "one\n")
			},
			change: func(t *testing.T, root string) {
				write(t, root, "ignored.txt", "one and two\n")
			},
			wantSame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := repo(t)
			tt.setup(t, root)
			params := ReadFingerprintParams{Root: root, GitDir: filepath.Join(root, ".git")}

			before, err := client.ReadFingerprint(t.Context(), params)
			require.NoError(t, err)

			tt.change(t, root)
			params.Hidden = &before.Hidden
			after, err := client.ReadFingerprint(t.Context(), params)
			require.NoError(t, err)

			assert.Equal(t, tt.wantSame, before.Key == after.Key)
		})
	}
}
