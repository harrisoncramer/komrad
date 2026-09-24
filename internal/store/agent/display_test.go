package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetHiddenIgnoresAnIDNoRowCouldHold pins the guard it opens with, which a string that is not an id at all would otherwise reach Postgres as a type error past.
func TestSetHiddenIgnoresAnIDNoRowCouldHold(t *testing.T) {
	changed, err := open(t).SetHidden(t.Context(), "not-a-uuid", true)

	require.NoError(t, err, "an id no row could hold is nothing to do rather than something to report")
	assert.False(t, changed, "nothing was hidden")
}
