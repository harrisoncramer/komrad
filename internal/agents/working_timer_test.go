package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFrameShowsWorkVariants pins the working detector against real status-line
// shapes. The critical case is a working line with no timer yet ("✽ Beboppin'…"),
// which appears at the start of every turn before tokens stream: requiring the
// timer used to miss it and leave working agents undetected. It also guards
// against chrome and prose being read as work.
func TestFrameShowsWorkVariants(t *testing.T) {
	// The detector keys on the "<glyph> <verb>…" shape, not a fixed glyph set, so
	// every spinner Claude rotates through reads as working regardless of glyph.
	work := []string{
		"✽ Beboppin'…", // no timer yet
		"✢ Composing… (32s · thinking more with high effort)", // timer with suffix
		"· Cooking… (1m 50s · ↓ 5.4k tokens)",
		"✶ Herding…",
		"✻ Cogitating… (12s)",
		"✳ Simmering…",
		"⠋ Thinking…", // a braille spinner
		"* Working…",
		"· Working… (3s · esc to interrupt)",
	}
	for _, f := range work {
		assert.True(t, frameShowsWork(f), "should read as working: %q", f)
	}

	notWork := []string{
		"⎿ Found… (3 files)",    // a tool-result line, not the spinner
		"⎿ Tip: run /foo",       // a tip line
		"wait… (2 more)",        // prose starting with a letter
		"done… (100%)",          // prose
		"❯ my message ends in…", // the input prompt echoing typed text
		"───────────────",       // a box rule
		"  -- INSERT -- ⏵⏵ bypass permissions on",
		"❯ ",
		"",
	}
	for _, f := range notWork {
		assert.False(t, frameShowsWork(f), "should not read as working: %q", f)
	}
}
