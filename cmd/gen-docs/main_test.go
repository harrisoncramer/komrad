package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunPrunesPagesWithoutCommands covers the invariant the reference directory rests on:
// every page in it is output of the current command tree, so a page left over from a command
// that has been removed or renamed is deleted rather than documenting something the binary
// no longer has.
func TestRunPrunesPagesWithoutCommands(t *testing.T) {
	dir := t.TempDir()

	orphan := filepath.Join(dir, "weave_gone.md")
	require.NoError(t, os.WriteFile(orphan, []byte("## weave gone\n"), 0o644))

	require.NoError(t, run(dir))

	require.NoFileExists(t, orphan)
	require.FileExists(t, filepath.Join(dir, "weave.md"))
}
