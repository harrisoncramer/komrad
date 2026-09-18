package view

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// update rewrites every golden file from what the renderer draws.
var update = flag.Bool("update", false, "rewrite the golden files")

// TestMain pins the colour profile, since lipgloss writes no escapes at all when nothing it can
// ask about the terminal answers.
func TestMain(m *testing.M) {
	flag.Parse()
	lipgloss.SetColorProfile(termenv.ANSI)
	lipgloss.SetHasDarkBackground(true)

	os.Exit(m.Run())
}

// requireGolden compares a drawn block against the file holding it, writing that file instead
// when the suite was run to update them.
func requireGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))

		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden file yet, so run the suite with -update")
	assert.Equal(t, string(want), got)
}
