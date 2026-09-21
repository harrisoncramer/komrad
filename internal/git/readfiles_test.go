//go:build integration

package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadFilesReadsEverySideInOneCommand covers the sources of a whole patch coming back
// keyed by commit and path, with a path the commit does not hold left out rather than failing.
func TestReadFilesReadsEverySideInOneCommand(t *testing.T) {
	t.Parallel()

	root := repo(t)
	write(t, root, "kept.go", "package main\n\nvar moved = 1\n")
	write(t, root, "added.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "second")

	base, err := Run(t.Context(), root, "rev-parse", "HEAD~1")
	require.NoError(t, err)

	head, err := Run(t.Context(), root, "rev-parse", "HEAD")
	require.NoError(t, err)

	files, err := client.ReadFiles(t.Context(), root, []FileRevision{
		{SHA: base, Path: "kept.go"},
		{SHA: head, Path: "kept.go"},
		{SHA: base, Path: "added.go"},
		{SHA: head, Path: "added.go"},
		{SHA: head, Path: "proto"},
	})
	require.NoError(t, err)

	assert.Equal(t, map[FileRevision][]byte{
		{SHA: base, Path: "kept.go"}:  []byte("package main\n"),
		{SHA: head, Path: "kept.go"}:  []byte("package main\n\nvar moved = 1\n"),
		{SHA: head, Path: "added.go"}: []byte("package main\n"),
	}, files)
}

// TestReadFilesOfNothingRunsNothing covers a patch with no file to colour.
func TestReadFilesOfNothingRunsNothing(t *testing.T) {
	t.Parallel()

	files, err := client.ReadFiles(t.Context(), t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, files)
}

// TestReadFilesRefusesADirectoryGitDoesNotHave covers the one failure that is git's rather than
// a file's.
func TestReadFilesRefusesADirectoryGitDoesNotHave(t *testing.T) {
	t.Parallel()

	_, err := client.ReadFiles(t.Context(), t.TempDir(), []FileRevision{{SHA: "HEAD", Path: "x"}})
	require.Error(t, err)
}
