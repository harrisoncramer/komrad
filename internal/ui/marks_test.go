package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsMarkLetter pins what may be a mark.
func TestIsMarkLetter(t *testing.T) {
	for _, key := range []string{"a", "m", "z"} {
		assert.True(t, isMarkLetter(key), "key %q", key)
	}

	for _, key := range []string{"A", "Z", "1", "esc", "ctrl+a", "'", "", "ab", "●"} {
		assert.False(t, isMarkLetter(key), "key %q", key)
	}
}
