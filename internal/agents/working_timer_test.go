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

// TestFrameShowsWorkThroughToolBlock pins the fix for a real miss: when Claude
// renders a plan/todo block, it sits below the status line and above the input
// box, pushing the status line a dozen-plus lines up from the bottom. Detection
// must still find it, and must find it whether or not the timer has appeared.
func TestFrameShowsWorkThroughToolBlock(t *testing.T) {
	block := "\n" +
		"  ⎿  ◼ Schema + migration + sqlc for workflows\n" +
		"     ◻ Store layer: Workflow type + WorkflowID + CRUD\n" +
		"     ◻ Proto + RPC service for workflows\n" +
		"     ◻ Manager: workflow ops + scoped session/search\n" +
		"     ◻ Panel: workflow scope, picker, move, back-nav\n" +
		"      … +1 pending\n\n" +
		"────────────────\n❯ \n────────────────\n   ~/dir  main\n  -- INSERT --"

	withTimer := "✢ Building schema, migration, sqlc… (4m 18s · ↓ 15.5k tokens · thinking with high effort)" + block
	noTimer := "✢ Building schema, migration, sqlc…" + block

	assert.True(t, frameShowsWork(withTimer), "status line above a tool block, with timer")
	assert.True(t, frameShowsWork(noTimer), "status line above a tool block, no timer yet (multi-word verb)")
}

// TestWaitingIgnoresProseInWorkWindow pins the split-window fix: the wide working
// window must not drag the waiting phrases ("esc to cancel", "enter to select")
// in from transcript text. Here those phrases sit a dozen lines up (inside the
// work window, outside the tight waiting window) while the bottom is an idle
// input box, so the frame is neither working nor waiting.
func TestWaitingIgnoresProseInWorkWindow(t *testing.T) {
	prose := "I split it so \"esc to cancel\" and \"Enter to select\" in a message do not read as a prompt."
	frame := prose + "\n\n\n\n\n\n\n\n\n\n───────\n❯ \n───────\n  -- INSERT --"

	assert.False(t, frameShowsWaiting(frame), "transcript phrases must not read as a live prompt")
	assert.False(t, frameShowsWork(frame), "an idle box is not working")
}
