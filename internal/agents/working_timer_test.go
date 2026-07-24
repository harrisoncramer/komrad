package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFrameShowsWorkVariants pins the working detector against real status-line
// shapes. The critical case is a working line with no timer yet, "✽ Beboppin'…",
// which appears at the start of every turn before tokens stream. The detector
// must catch it without the timer, and must not read chrome or prose as work.
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
	for _, frame := range work {
		assert.True(t, frameShowsWork(frame), "should read as working: %q", frame)
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
	for _, frame := range notWork {
		assert.False(t, frameShowsWork(frame), "should not read as working: %q", frame)
	}
}

// TestFrameShowsWorkThroughToolBlock checks a plan or todo block rendered below
// the status line and above the input box, which pushes the status line a
// dozen-plus lines up from the bottom. Detection must still find it, with or
// without the timer.
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

// TestWaitingIgnoresProseInWorkWindow checks that the wide working window does not
// drag the waiting phrases "esc to cancel" and "enter to select" in from
// transcript text. Those phrases sit a dozen lines up, inside the work window but
// outside the tight waiting window, while the bottom is an idle input box, so the
// frame is neither working nor waiting.
func TestWaitingIgnoresProseInWorkWindow(t *testing.T) {
	prose := "I split it so \"esc to cancel\" and \"Enter to select\" in a message do not read as a prompt."
	frame := prose + "\n\n\n\n\n\n\n\n\n\n───────\n❯ \n───────\n  -- INSERT --"

	assert.False(t, frameShowsWaiting(frame), "transcript phrases must not read as a live prompt")
	assert.False(t, frameShowsWork(frame), "an idle box is not working")
}

// TestFrameShowsWorkStripsANSI checks detection against real color captures. The
// pane is captured with colors, so the status line arrives shredded by SGR
// escapes, with the verb split across color runs and codes wedged between the
// ellipsis and the timer. Detection must strip them first or it matches nothing.
func TestFrameShowsWorkStripsANSI(t *testing.T) {
	work := []string{
		// "✽ Flambéing… (3m 30s · ↓ 11.6k tokens)" as captured with -e
		"\x1b[38;5;174m✽\x1b[39m \x1b[38;5;174mFl\x1b[38;5;216mamb\x1b[38;5;174méing…\x1b[39m \x1b[38;5;246m(3m 30s · ↓\x1b[39m \x1b[38;5;246m11.6k tokens)\x1b[39m",
		// a multi-word verb split across color runs, no timer
		"\x1b[38;5;174m✢\x1b[39m \x1b[1mBuild\x1b[22ming panel\x1b[38;5;246m…\x1b[39m",
	}
	for _, frame := range work {
		assert.True(t, frameShowsWork(frame), "ANSI-wrapped status line should read as working")
	}
}
