package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChecksAreWellFormed covers the wiring of the list rather than any one assertion: a
// typo in a dependency name would silently run a check meant to be skipped, and a duplicate
// name would read the wrong check's conclusion, neither of which shows up at runtime as
// more than a confusing report.
func TestChecksAreWellFormed(t *testing.T) {
	checks := Checks(Settings{Harnesses: []HarnessInfo{
		{ID: "claude", Binary: "claude"},
		{ID: "codex", Binary: "codex"},
	}})
	require.NotEmpty(t, checks)

	seen := make(map[string]bool, len(checks))
	for _, check := range checks {
		assert.NotEmpty(t, check.Name)
		assert.NotEmpty(t, check.Description, "%s says what it is for", check.Name)
		assert.NotNil(t, check.Run, "%s has something to run", check.Name)

		assert.False(t, seen[check.Name], "no two checks answer to %q", check.Name)
		seen[check.Name] = true
	}

	concluded := make(map[string]bool, len(checks))
	for _, check := range checks {
		for _, needed := range check.Needs {
			assert.True(t, concluded[needed],
				"%q needs %q, which has to be a check listed before it", check.Name, needed)
		}
		concluded[check.Name] = true
	}
}
