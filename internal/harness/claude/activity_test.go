package claude

import (
	"strings"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
)

const (
	busyFrame  = "· Cooking… (1m 50s · ↓ 5.4k tokens)\n───────\n❯ \n───────"
	idleFrame  = "───────────────\n❯ \n───────────────\n  -- INSERT -- ⏵⏵ bypass permissions on"
	typedFrame = "───────────────\n❯ half a question about\n───────────────\n  -- INSERT -- ⏵⏵ bypass permissions on"
	// suggestedFrame is the box Claude draws after a turn ends, holding the prompt it suggests
	// next in the dim it draws its own furniture in.
	suggestedFrame = "───────────────\n\x1b[39m❯ \x1b[2mcheck if the author fixed anything yet\x1b[0m\n───────────────\n  -- INSERT --"
	waitFrame      = "Teleport anywhere for one day. Where?\n❯ 1. Tokyo\n  2. Patagonia\n\nEnter to select · ↑/↓ to navigate · Esc to cancel"
)

// wideRule is a rule drawn to the full width of an eighty column pane.
const wideRule = "────────────────────────────────────────────────────────────────────────────────"

// boxRuleLine is the rule Claude draws across an eighty column pane, as tmux captures it.
const boxRuleLine = "\x1b[38;5;102m────────────────────────────────────────────────────────────────────────────\x1b[39m"

// wrappedSuggestionFrame is a suggestion long enough to wrap, captured out of a real tmux pane,
// where the continuation line carries no dim of its own because tmux re-emits no attribute at a
// wrap.
const wrappedSuggestionFrame = boxRuleLine + "\n" +
	"❯ \x1b[2mcheck whether the reviewer came back with anything yet and summarise what it s\n" +
	"aid\x1b[0m\n" +
	boxRuleLine + "\n" +
	"  \x1b[38;5;102m-- INSERT --\x1b[39m \x1b[38;5;203m⏵⏵ bypass permissions on\x1b[39m"

// dimRuleFrame is a draft under a full width dim rule, captured out of a real tmux pane, where
// the rule leaves its dim open at the end of its own line and the reset lands on the first cell
// of the line the draft is on.
const dimRuleFrame = "\x1b[2m" + wideRule + "\n" +
	"\x1b[0m❯ half a question about\n" +
	"\x1b[2m" + wideRule + "\n" +
	"\x1b[0m  -- INSERT --"

// retryFrame is the banner Claude draws over an empty box while it retries an API error,
// captured out of a real tmux pane during a repeated 529.
const retryFrame = "\x1b[38;5;203m\x1b[49m✻\x1b[39m \x1b[38;5;203mAPI error\x1b[38;5;102m · Retrying in 1s · attempt 1/10\x1b[39m\n" +
	"\n" + idleFrame

// codedRetryFrame is the same banner once Claude has a status code and a hint to name, where
// the text after the glyph opens with the code rather than with a word.
const codedRetryFrame = "✻ 529 Overloaded · Retrying in 11s · attempt 6/10\n" +
	"  ⎿  If it persists, check your inference gateway (127.0.0.1:8799).\n" +
	"\n" + idleFrame

const toolBlock = "\n" +
	"  ⎿  ◼ Schema + migration + sqlc for workflows\n" +
	"     ◻ Store layer: Workflow type + WorkflowID + CRUD\n" +
	"     ◻ Panel: workflow scope, picker, move, back-nav\n" +
	"      … +1 pending\n\n" +
	"────────────────\n❯ \n────────────────\n   ~/dir  main\n  -- INSERT --"

// TestReadActivity covers the verdict a single captured frame settles on.
func TestReadActivity(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		want  harness.Activity
	}{
		{
			name:  "an unreadable capture says nothing either way",
			frame: "",
			want:  harness.ActivityUnknown,
		},
		{
			name:  "confirmation prompt is not working",
			frame: waitFrame,
			want:  harness.ActivityWaiting,
		},
		{
			name:  "finished summary line is not working",
			frame: "✻ Brewed for 2m 36s",
			want:  harness.ActivityUnrecognized,
		},
		{
			name:  "idle input box is neither working nor waiting",
			frame: idleFrame,
			want:  harness.ActivityIdle,
		},
		{
			name:  "an input box somebody has typed into",
			frame: typedFrame,
			want:  harness.ActivityComposing,
		},
		{
			name:  "an input box holding text under the marker rather than beside it",
			frame: "───────────────\n❯ \n  a second paragraph\n───────────────\n  -- INSERT --",
			want:  harness.ActivityComposing,
		},
		{
			name:  "an empty box, whose marker is followed by the space Claude actually draws",
			frame: "───────────────\n\x1b[39m❯\u00a0\n───────────────\n  -- INSERT --",
			want:  harness.ActivityIdle,
		},
		{
			name:  "a transcript echoing a sent message above an empty box",
			frame: "❯ what did I ask again\n\n⏺ You asked this.\n───────────────\n❯ \n───────────────\n  -- INSERT --",
			want:  harness.ActivityIdle,
		},
		{
			name:  "the prompt Claude suggests in an empty box is not somebody's draft",
			frame: suggestedFrame,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a suggestion long enough to wrap under the marker is still not a draft",
			frame: wrappedSuggestionFrame,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a draft under a dim rule that left its dim open at the wrap",
			frame: dimRuleFrame,
			want:  harness.ActivityComposing,
		},
		{
			name:  "a draft typed under a box rule Claude drew dim",
			frame: "\x1b[2m───────────────\x1b[0m\n❯ half a question about\n\x1b[2m───────────────\x1b[0m\n  -- INSERT --",
			want:  harness.ActivityComposing,
		},
		{
			name:  "an input box whose marker carries colour",
			frame: "───────────────\n\x1b[39m❯ still writing\n───────────────\n  -- INSERT --",
			want:  harness.ActivityComposing,
		},
		{
			name:  "a tool-result line rather than the spinner",
			frame: "⎿ Found… (3 files)",
			want:  harness.ActivityUnrecognized,
		},
		{
			name:  "prose that happens to end in an ellipsis",
			frame: "wait… (2 more)",
			want:  harness.ActivityUnrecognized,
		},
		{
			name:  "the input prompt echoing typed text",
			frame: "❯ my message ends in…",
			want:  harness.ActivityUnrecognized,
		},
		{
			name:  "a box rule",
			frame: "───────────────",
			want:  harness.ActivityUnrecognized,
		},
		{
			name:  "a verb with no timer yet, which is every turn's first frame",
			frame: "✽ Beboppin'…",
			want:  harness.ActivityWorking,
		},
		{
			name:  "a braille spinner",
			frame: "⠋ Thinking…",
			want:  harness.ActivityWorking,
		},
		{
			name:  "a status line above a tool block, with a timer",
			frame: "✢ Building schema, migration, sqlc… (4m 18s · ↓ 15.5k tokens)" + toolBlock,
			want:  harness.ActivityWorking,
		},
		{
			name:  "a multi-word verb above a tool block, no timer yet",
			frame: "✢ Building schema, migration, sqlc…" + toolBlock,
			want:  harness.ActivityWorking,
		},
		{
			name:  "a line shredded by ANSI colour codes",
			frame: "\x1b[38;5;174m✽\x1b[39m \x1b[38;5;174mFl\x1b[38;5;216mamb\x1b[38;5;174méing…\x1b[39m \x1b[38;5;246m(3m 30s · ↓\x1b[39m \x1b[38;5;246m11.6k tokens)\x1b[39m",
			want:  harness.ActivityWorking,
		},
		{
			name:  "token-count timer reads as working",
			frame: "· Cooking… (1m 50s · ↓ 5.4k tokens)",
			want:  harness.ActivityWorking,
		},
		{
			name:  "short verb timer reads as working",
			frame: "✽ Cogitating… (12s)",
			want:  harness.ActivityWorking,
		},
		{
			name:  "bare ellipsis timer reads as working",
			frame: "…(0s",
			want:  harness.ActivityWorking,
		},
		{
			name:  "the retry countdown over an empty box is a turn still running",
			frame: retryFrame,
			want:  harness.ActivityWorking,
		},
		{
			name:  "a retry banner naming a status code rather than a word",
			frame: codedRetryFrame,
			want:  harness.ActivityWorking,
		},
		{
			name:  "the summary of a turn that died to an API error is not a retry",
			frame: "⏺ API Error: Repeated 529 Overloaded errors. The API is at capacity.\n\n✻ Worked for 1s · done 11:43 AM\n" + idleFrame,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a busy frame is working rather than waiting",
			frame: busyFrame,
			want:  harness.ActivityWorking,
		},
		{
			name:  "yes/no confirmation is waiting",
			frame: "Do you want to proceed?\n❯ 1. Yes\n  2. No\nEnter to select · Esc to cancel",
			want:  harness.ActivityWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).ReadActivity(tt.frame))
		})
	}
}

// TestReadActivityIgnoresTranscriptProse checks transcript text carrying a marker phrase is not
// read as a live prompt, since only the bottom of the frame counts.
func TestReadActivityIgnoresTranscriptProse(t *testing.T) {
	tests := []struct {
		name  string
		frame string
	}{
		{
			name:  "prose well above the live region",
			frame: "I match `Enter to select` and `Do you want`, and the timer `· Cooking… (1m 50s)`." + strings.Repeat("\n", liveRegionLines) + idleFrame,
		},
		{
			name:  "a retry countdown quoted in prose is not a countdown",
			frame: "I match `✻ 529 Overloaded · Retrying in 11s · attempt 6/10`, badly." + strings.Repeat("\n", liveRegionLines) + idleFrame,
		},
		{
			name:  "waiting phrases a dozen lines up",
			frame: `I split it so "esc to cancel" and "Enter to select" in a message do not read as a prompt.` + "\n\n\n\n\n\n\n\n\n\n───────\n❯ \n───────\n  -- INSERT --",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, harness.ActivityIdle, NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).ReadActivity(tt.frame),
				"prose must not read as a live prompt or as work")
		})
	}
}
