package codex

import (
	"strings"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
)

// The frames below are real captures from codex-cli 0.144.4 running in a tmux pane.
// idleFrame is the input box it sits at, with the placeholder it shows when nothing has
// been typed and the footer naming the model and directory.
const idleFrame = "• ls returned no files. Total: 0.\n" +
	"────────────────────────────────────────\n" +
	"\n" +
	"› Improve documentation in @filename\n" +
	"\n" +
	"  gpt-5.6-sol default fast · /repo/worktree"

// workingFrame is the live status line, which carries the timer from its first frame
// rather than growing one once tokens stream.
const workingFrame = "› list the files in this directory\n" +
	"\n" +
	"• Working (5s • esc to interrupt) · 1 background terminal running · /ps to view\n" +
	"\n" +
	"› Improve documentation in @filename\n" +
	"  gpt-5.6-sol default fast · /repo/worktree"

// approvalFrame is a command Codex is blocked on. patchFrame is the same question about
// an edit, and both close with the identical footer.
const approvalFrame = "  Would you like to run the following command?\n" +
	"\n" +
	"  Environment: local\n" +
	"\n" +
	"  $ touch /tmp/probe\n" +
	"\n" +
	"› 1. Yes, proceed (y)\n" +
	"  2. Yes, and don't ask again for commands that start with `touch` (p)\n" +
	"  3. No, and tell Codex what to do differently (esc)\n" +
	"\n" +
	"  Press enter to confirm or esc to cancel"

const patchFrame = "• Added note.txt (+1 -0)\n" +
	"    1 +hello\n" +
	"\n" +
	"  Would you like to make the following edits?\n" +
	"› 1. Yes, proceed (y)\n" +
	"  2. Yes, and don't ask again for these files (a)\n" +
	"  3. No, and tell Codex what to do differently (esc)\n" +
	"\n" +
	"  Press enter to confirm or esc to cancel"

// trustFrame is the question Codex asks the first time it runs in a directory, and
// loginFrame is the sign-in screen. Both are the user's turn and both close with the
// same wording, which is why one pattern covers all four blocking states.
const trustFrame = "> You are in /repo/worktree\n" +
	"\n" +
	"  Do you trust the contents of this directory?\n" +
	"\n" +
	"› 1. Yes, continue\n" +
	"  2. No, quit\n" +
	"\n" +
	"  Press enter to continue"

const loginFrame = "  Welcome to Codex, OpenAI's command-line coding agent\n" +
	"  Sign in with ChatGPT to use Codex as part of your paid plan\n" +
	"\n" +
	"> 1. Sign in with ChatGPT\n" +
	"  2. Sign in with Device Code\n" +
	"  3. Provide your own API key\n" +
	"\n" +
	"  Press enter to continue"

// TestReadFrame covers the verdict one captured frame settles on. Codex prefixes its
// own prose and every tool line with the same bullet the status line uses, so most of
// these cases are about not reading ordinary transcript text as work.
func TestReadFrame(t *testing.T) {
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
			name:  "the input box is idle",
			frame: idleFrame,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a bulleted assistant message is not work",
			frame: "• I'll run ls in the current directory and count what it lists.",
			want:  harness.ActivityIdle,
		},
		{
			name:  "a bulleted tool line is not work",
			frame: "• Running ls | tee /tmp/out.txt; wc -l < /tmp/out.txt",
			want:  harness.ActivityIdle,
		},
		{
			name:  "a finished tool line is not work",
			frame: "• Ran ls | tee /tmp/out.txt\n  └        0",
			want:  harness.ActivityIdle,
		},
		{
			name:  "an approval the user already answered is not work",
			frame: "✔ You approved codex to run ls this time",
			want:  harness.ActivityIdle,
		},
		{
			// The typed prompt stays in the input box and the status line goes, which is
			// what an escaped turn leaves behind.
			name:  "an interrupted turn reads as finished",
			frame: "› write a haiku about tmux\n  gpt-5.6-sol default fast · /repo/worktree",
			want:  harness.ActivityIdle,
		},
		{
			name:  "a slash-command popup is the user typing, not the agent working",
			frame: "› /model\n  /model  choose what model and reasoning effort to use",
			want:  harness.ActivityIdle,
		},
		{
			name:  "the status line is work",
			frame: workingFrame,
			want:  harness.ActivityWorking,
		},
		{
			name:  "the first frame of a turn, before any elapsed time",
			frame: "• Working (0s • esc to interrupt)",
			want:  harness.ActivityWorking,
		},
		{
			name:  "the interrupt hint alone is work, for a release that renames the verb",
			frame: "• Cogitating (2s • esc to interrupt)",
			want:  harness.ActivityWorking,
		},
		{
			name:  "the verb and timer alone are work, for a release that drops the hint",
			frame: "• Working (12s)",
			want:  harness.ActivityWorking,
		},
		{
			name:  "a command approval is waiting",
			frame: approvalFrame,
			want:  harness.ActivityWaiting,
		},
		{
			name:  "an edit approval is waiting",
			frame: patchFrame,
			want:  harness.ActivityWaiting,
		},
		{
			name:  "the directory-trust question is waiting",
			frame: trustFrame,
			want:  harness.ActivityWaiting,
		},
		{
			name:  "the sign-in screen is waiting",
			frame: loginFrame,
			want:  harness.ActivityWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, readFrame(tt.frame))
		})
	}
}

// TestReadFrameIgnoresTranscriptProse checks that an agent discussing the markers is
// not read as being at one. Only the bottom of the frame counts, and below the prose in
// both cases is an idle input box.
func TestReadFrameIgnoresTranscriptProse(t *testing.T) {
	tests := []struct {
		name  string
		frame string
	}{
		{
			name:  "prose above the working window",
			frame: "The status line reads `• Working (5s • esc to interrupt)`." + strings.Repeat("\n", liveRegionLines) + idleFrame,
		},
		{
			// Inside the wide work window but outside the tight waiting one, which is why
			// the two windows are sized differently.
			name:  "an approval phrase a dozen lines up",
			frame: "I wrote that it says \"Press enter to confirm or esc to cancel\"." + strings.Repeat("\n", 12) + idleFrame,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, harness.ActivityIdle, readFrame(tt.frame),
				"prose must not read as a live prompt or as work")
		})
	}
}

// TestReadFrameStripsANSI covers a frame captured with colours, which is how the
// panel captures every pane. The status line comes back shredded by SGR codes.
func TestReadFrameStripsANSI(t *testing.T) {
	frame := "\x1b[38;5;39m•\x1b[39m \x1b[1mWorking\x1b[0m \x1b[2m(7s • esc to interrupt)\x1b[0m"

	assert.Equal(t, harness.ActivityWorking, readFrame(frame))
}
