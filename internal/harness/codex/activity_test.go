package codex

import (
	"strings"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
)

const idleFrame = "• ls returned no files. Total: 0.\n" +
	"────────────────────────────────────────\n" +
	"\n" +
	"› Improve documentation in @filename\n" +
	"\n" +
	"  gpt-5.6-sol default fast · /repo/worktree"

const composerTail = "\n\n› Improve documentation in @filename\n\n  gpt-5.6-sol default fast · /repo/worktree"

const workingFrame = "› list the files in this directory\n" +
	"\n" +
	"• Working (5s • esc to interrupt) · 1 background terminal running · /ps to view\n" +
	"\n" +
	"› Improve documentation in @filename\n" +
	"  gpt-5.6-sol default fast · /repo/worktree"

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

const questionFrame = "  Question 1/1 (1 unanswered)\n" +
	"  Which filename should I use for the new file?\n" +
	"\n" +
	"  › 1. a.txt (Recommended)  Creates the new file as a.txt.\n" +
	"    2. b.txt                Creates the new file as b.txt.\n" +
	"    3. None of the above    Optionally, add details in notes (tab).\n" +
	"\n" +
	"  tab to add notes | enter to submit answer | esc to interrupt"

const questionNotesFrame = "  Question 1/1 (1 unanswered)\n" +
	"  Which filename should I use for the new file?\n" +
	"\n" +
	"  › 1. a.txt (Recommended)  Creates the new file as a.txt.\n" +
	"    2. b.txt                Creates the new file as b.txt.\n" +
	"\n" +
	"  › Add notes\n" +
	"\n" +
	"  tab or esc to clear notes | enter to submit answer"

const questionsFrame = "  Question 1/3 (3 unanswered)\n" +
	"  Which filename should I use?\n" +
	"\n" +
	"  › 1. probe.txt (Recommended)  Use a simple text file named probe.txt.\n" +
	"    2. output.txt               Use a generic output.txt filename.\n" +
	"    3. None of the above        Optionally, add details in notes (tab).\n" +
	"\n" +
	"  tab to add notes | enter to submit answer | ←/→ to navigate questions | esc to interrupt"

const quotedStatusFrame = "  \u2514 weave.send_mail({\"body\":\"the pane still said Working (4s \u2022 esc to interrupt) afterwards\"})\n" +
	"\n" +
	"\u2022 Yes. It is a separate bug that already exists on main.\n" +
	"\n" +
	"\u2500 Worked for 3m 48s \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n" +
	"\n" +
	"\u203a Explain this codebase\n" +
	"\n" +
	"  gpt-5.6-sol medium fast · /repo/worktree"

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

// TestReadActivity covers the verdict one captured frame settles on, mostly about not reading
// prose as work since Codex prefixes it with the same bullet the status line uses.
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
			name:  "the input box is idle",
			frame: idleFrame,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a bulleted assistant message is not work",
			frame: "• I'll run ls in the current directory and count what it lists." + composerTail,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a bulleted tool line is not work",
			frame: "• Running ls | tee /tmp/out.txt; wc -l < /tmp/out.txt" + composerTail,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a finished tool line is not work",
			frame: "• Ran ls | tee /tmp/out.txt\n  └        0" + composerTail,
			want:  harness.ActivityIdle,
		},
		{
			name:  "an approval the user already answered is not work",
			frame: "✔ You approved codex to run ls this time" + composerTail,
			want:  harness.ActivityIdle,
		},
		{
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
			name:  "a finished turn that quoted the status line is not work",
			frame: quotedStatusFrame,
			want:  harness.ActivityIdle,
		},
		{
			name:  "an approval phrase just above a live status line",
			frame: "• The prompt says press enter to continue.\n\n• Working (4s • esc to interrupt)\n\n› Improve documentation in @filename\n\n  gpt-5.6-sol default fast · /repo/worktree",
			want:  harness.ActivityWorking,
		},
		{
			name:  "an answered question is work again",
			frame: "    answer: a.txt (Recommended)\n\n• Working (4s • esc to interrupt)\n\n› Summarize recent commits\n\n  gpt-5.6-sol medium fast · /repo/worktree",
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
			name:  "a question Codex asked with its own tool is waiting",
			frame: questionFrame,
			want:  harness.ActivityWaiting,
		},
		{
			name:  "one of several questions is waiting",
			frame: questionsFrame,
			want:  harness.ActivityWaiting,
		},
		{
			name:  "a question with the notes field open is waiting",
			frame: questionNotesFrame,
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
			assert.Equal(t, tt.want, NewHarness(NewHarnessParams{SessionsDir: t.TempDir()}).ReadActivity(tt.frame))
		})
	}
}

// TestReadActivityIgnoresTranscriptProse checks an agent discussing the markers is not read as
// being at one, since only the bottom of the frame counts.
func TestReadActivityIgnoresTranscriptProse(t *testing.T) {
	tests := []struct {
		name  string
		frame string
	}{
		{
			name:  "prose above the working window",
			frame: "The status line reads `• Working (5s • esc to interrupt)`." + strings.Repeat("\n", liveRegionLines) + idleFrame,
		},
		{
			name:  "an approval phrase a dozen lines up",
			frame: "I wrote that it says \"Press enter to confirm or esc to cancel\"." + strings.Repeat("\n", 12) + idleFrame,
		},
		{
			name:  "a question footer a dozen lines up",
			frame: "I wrote that it ends with \"enter to submit answer\"." + strings.Repeat("\n", 12) + idleFrame,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, harness.ActivityIdle, NewHarness(NewHarnessParams{SessionsDir: t.TempDir()}).ReadActivity(tt.frame),
				"prose must not read as a live prompt or as work")
		})
	}
}

// TestReadActivityStripsANSI covers a frame captured with colours, where the status line comes
// back shredded by SGR codes.
func TestReadActivityStripsANSI(t *testing.T) {
	frame := "\x1b[38;5;39m•\x1b[39m \x1b[1mWorking\x1b[0m \x1b[2m(7s • esc to interrupt)\x1b[0m"

	assert.Equal(t, harness.ActivityWorking, NewHarness(NewHarnessParams{SessionsDir: t.TempDir()}).ReadActivity(frame))
}

// TestReadActivityWithoutComposer covers a pane whose Codex has not drawn its input box, where
// reading it as idle would let mail be typed at a pane that is not listening.
func TestReadActivityWithoutComposer(t *testing.T) {
	tests := []struct {
		name  string
		frame string
	}{
		{
			name:  "the shell's echo of the launch command",
			frame: "harry@mac ~/weave % WEAVE_AGENT_ID=01a0 codex resume 019abc",
		},
		{
			name:  "a startup banner with no composer",
			frame: "  Welcome to Codex, OpenAI's command-line coding agent\n  Loading session 019abc",
		},
		{
			name:  "a pane that has drawn nothing but blank lines",
			frame: "\n\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, harness.ActivityUnrecognized, NewHarness(NewHarnessParams{SessionsDir: t.TempDir()}).ReadActivity(tt.frame))
		})
	}
}
