package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReferenceIsInStepWithSources fails when the committed data under docs/src/data is not what the sources generate.
func TestReferenceIsInStepWithSources(t *testing.T) {
	const committed = "../../docs/src/data"

	fresh := t.TempDir()
	require.NoError(t, run(t.Context(), "../..", fresh))

	for _, name := range []string{"api.json", "cli.json", "config.json", "glyphs.json"} {
		require.Equal(t, read(t, filepath.Join(fresh, name)), read(t, filepath.Join(committed, name)), "%s is stale; run mise run gen:docs and commit the result", name)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()

	contents, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(contents)
}
