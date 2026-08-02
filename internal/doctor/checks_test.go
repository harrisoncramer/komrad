package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChecksAreWellFormed covers the wiring of the list rather than any one assertion.
//
// A dependency is matched by name, so a typo in one would silently run a check that was
// meant to be skipped, and a duplicated name would make the whole mechanism read the wrong
// check's conclusion. Neither shows up at runtime as anything but a confusing report.
func TestChecksAreWellFormed(t *testing.T) {
	checks := Checks()
	require.NotEmpty(t, checks)

	seen := make(map[string]bool, len(checks))
	for _, check := range checks {
		assert.NotEmpty(t, check.Name)
		assert.NotEmpty(t, check.Description, "%s says what it is for", check.Name)
		assert.NotNil(t, check.Run, "%s has something to run", check.Name)

		assert.False(t, seen[check.Name], "no two checks answer to %q", check.Name)
		seen[check.Name] = true
	}

	// Walked after the names are collected only for the duplicate test above; a
	// dependency has to be declared before its dependent to be concluded in time.
	concluded := make(map[string]bool, len(checks))
	for _, check := range checks {
		for _, needed := range check.Needs {
			assert.True(t, concluded[needed],
				"%q needs %q, which has to be a check listed before it", check.Name, needed)
		}
		concluded[check.Name] = true
	}
}
