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

// TestRunLeavesWhatItCannotHaveWritten covers the other half of the prune: cobra writes flat
// markdown, so a directory or a file of any other kind was put there by a person and deleting
// it would be reading the rule further than the generator can vouch for.
func TestRunLeavesWhatItCannotHaveWritten(t *testing.T) {
	dir := t.TempDir()

	assets := filepath.Join(dir, "assets")
	require.NoError(t, os.Mkdir(assets, 0o755))

	notes := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("by hand\n"), 0o644))

	require.NoError(t, run(dir))

	require.DirExists(t, assets)
	require.FileExists(t, notes)
}
