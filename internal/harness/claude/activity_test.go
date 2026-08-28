package claude

import (
	"strings"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
)

const (
	busyFrame = "· Cooking… (1m 50s · ↓ 5.4k tokens)\n───────\n❯ \n───────"
	idleFrame = "───────────────\n❯ \n───────────────\n  -- INSERT -- ⏵⏵ bypass permissions on"
	waitFrame = "Teleport anywhere for one day. Where?\n❯ 1. Tokyo\n  2. Patagonia\n\nEnter to select · ↑/↓ to navigate · Esc to cancel"
)

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
