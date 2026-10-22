package paneltest

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateGoldens = flag.Bool("update", false, "rewrite the golden files")

// RequireGolden compares a rendered block against the file named for it under testdata.
func RequireGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")
	if *updateGoldens {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))

		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden file yet, so run the suite with -update")
	assert.Equal(t, string(want), got)
}
