package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func pressMark(t *testing.T, m model, keys ...string) model {
	t.Helper()

	var next tea.Model = m
	for _, key := range keys {
		updated, _ := next.(model).Update(keyMsg(key))
		next = updated
	}

	after, ok := next.(model)
	require.True(t, ok)

	return after
}
