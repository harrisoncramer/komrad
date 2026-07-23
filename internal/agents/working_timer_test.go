package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFrameShowsWorkRejectsIncidentalParens pins the tightened timer match: a
// bottom-region line that has an ellipsis and a parenthesised number but is not
// an elapsed timer (no seconds unit) must not be read as work, or an idle agent
// with such text would be promoted to thinking and stuck there.
func TestFrameShowsWorkRejectsIncidentalParens(t *testing.T) {
	notWork := []string{
		"⎿ Found… (3 files)",
		"wait… (2 more)",
		"see the note… (1)",
		"done… (100%)",
	}
	for _, f := range notWork {
		assert.False(t, frameShowsWork(f), "incidental parens must not read as work: %q", f)
	}

	work := []string{
		"· Cooking… (12s)",
		"✽ Determining… (1m 50s · ↓ 5.4k tokens)",
		"· Working… (3s · esc to interrupt)",
	}
	for _, f := range work {
		assert.True(t, frameShowsWork(f), "a real elapsed timer must read as work: %q", f)
	}
}
