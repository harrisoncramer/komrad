package codex

import (
	"strings"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
)

// A placeholder Codex draws in an empty composer is dim, and a frame is captured with its
// colours, so the escapes are part of what a fixture has to carry.
const (
	dimOn  = "\x1b[2m"
	dimOff = "\x1b[0m"
)

const composerHint = dimOn + "Improve documentation in @filename" + dimOff

const idleFrame = "• ls returned no files. Total: 0.\n" +
	"────────────────────────────────────────\n" +
	"\n" +
	"› " + composerHint + "\n" +
	"\n" +
	"  gpt-5.6-sol default fast · /repo/worktree"

// startupFrame is a codex composer captured out of a real tmux pane at eighty columns, where
// codex truncates its own footer rather than letting it wrap.
const startupFrame = "\x1b[2m╰─────────────────────────────────────────────────────╯\x1b[0m\n" +
	"\n" +
	"  \x1b[1mTip:\x1b[0m \x1b[3mNew\x1b[0m Use \x1b[1m/fast\x1b[0m to enable our fastest inference with increased plan usage.\n" +
	"\n" +
	"\n" +
	"\x1b[1m›\x1b[0m \x1b[2mRun /review on my current changes\x1b[0m\n" +
	"\n" +
	"  \x1b[38;2;130;156;207mgpt-5.6-sol medium\x1b[2m\x1b[39m · \x1b[0m\x1b[38;2;154;184;117m~/.local/state/weave/worktrees/01a06355-b7ca-78b2-b18e-f…"

const composerTail = "\n\n› " + composerHint + "\n\n  gpt-5.6-sol default fast · /repo/worktree"

const workingFrame = "› list the files in this directory\n" +
	"\n" +
	"• Working (5s • esc to interrupt) · 1 background terminal running · /ps to view\n" +
	"\n" +
	"› " + composerHint + "\n" +
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

// labelledStartupFrame is the line a startup step draws before the model is reached, where the
// label holds words and parens of its own and the timer is drawn dim, as tmux captures it.
const labelledStartupFrame = "\x1b[1m\x1b[38;2;128;128;128m•\x1b[0m \x1b[1m\x1b[38;2;128;128;128mStarting MCP servers (1/3): gadgets, codex_apps\x1b[0m " +
	dimOn + "(0s • esc to interrupt)" + dimOff + composerTail

const quotedStatusFrame = dimOn + "  \u2514 weave.send_mail({\"body\":\"the pane still said Working (4s \u2022 esc to interrupt) afterwards\"})" + dimOff + "\n" +
	"\n" +
	"\u2022 Yes. It is a separate bug that already exists on main.\n" +
	"\n" +
	"\u2500 Worked for 3m 48s \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n" +
	"\n" +
	"\u203a " + dimOn + "Explain this codebase" + dimOff + "\n" +
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
			name:  "the composer as a real pane draws it before a first turn",
			frame: startupFrame,
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
			name:  "a composer somebody has typed into",
			frame: "• ls returned no files. Total: 0.\n\n\x1b[1m›\x1b[0m half a question about\n\n  gpt-5.6-sol default fast · /repo/worktree",
			want:  harness.ActivityComposing,
		},
		{
			name:  "a composer holding a prompt nobody has sent yet",
			frame: "› write a haiku about tmux\n  gpt-5.6-sol default fast · /repo/worktree",
			want:  harness.ActivityComposing,
		},
		{
			name:  "a composer holding text under the marker rather than beside it",
			frame: "• ls returned no files. Total: 0.\n\n\x1b[1m›\x1b[0m\n  a second paragraph\n\n  gpt-5.6-sol medium · /private/tmp",
			want:  harness.ActivityComposing,
		},
		{
			name:  "text under the marker, under a footer that is dim from end to end",
			frame: "• 40\n\n\x1b[1m›\x1b[0m\n  second line text\n\n" + dimOn + "  esc again to edit previous message" + dimOff,
			want:  harness.ActivityComposing,
		},
		{
			name:  "a transcript echoing a sent message above an empty composer",
			frame: "› reply with exactly: ok\n\n• ok\n\n› " + composerHint + "\n\n  gpt-5.6-sol medium · /private/tmp",
			want:  harness.ActivityIdle,
		},
		{
			name:  "a slash-command popup is the user typing, not the agent working",
			frame: "› /model\n  /model  choose what model and reasoning effort to use",
			want:  harness.ActivityComposing,
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
			frame: "• Cogitating " + dimOn + "(2s • esc to interrupt)" + dimOff,
			want:  harness.ActivityWorking,
		},
		{
			name:  "the verb and timer alone are work, for a release that drops the hint",
			frame: "• Working (12s)",
			want:  harness.ActivityWorking,
		},
		{
			name:  "tool output drawn dim from end to end is not work, however it reads",
			frame: dimOn + "  └ testdata/starting.txt: • Starting MCP servers (1/3): gadgets (0s • esc to interrupt)" + dimOff + composerTail,
			want:  harness.ActivityIdle,
		},
		{
			name:  "an indent drawn bright above dim tool output is not work either",
			frame: "    " + dimOn + "• Cogitating (2s • esc to interrupt)" + dimOff + composerTail,
			want:  harness.ActivityIdle,
		},
		{
			name:  "the interrupt hint drawn bright is a message quoting it rather than work",
			frame: "• Cogitating (2s • esc to interrupt)" + composerTail,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a startup step naming what it is doing before the timer is work",
			frame: labelledStartupFrame,
			want:  harness.ActivityWorking,
		},
		{
			name:  "a sent message ending at a quoted timer is not work",
			frame: "› explain (0s • esc to interrupt)\n\n• Sure." + composerTail,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a draft ending at a quoted timer is not work",
			frame: "• 40\n\n\x1b[1m›\x1b[0m explain (0s • esc to interrupt)\n\n  gpt-5.6-sol medium · /private/tmp",
			want:  harness.ActivityComposing,
		},
		{
			name:  "prose wrapping onto a quoted timer is not work",
			frame: "• The status line said (0s • esc to interrupt)\n  and then stopped." + composerTail,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a finished turn that quoted the status line is not work",
			frame: quotedStatusFrame,
			want:  harness.ActivityIdle,
		},
		{
			name:  "an approval phrase just above a live status line",
			frame: "• The prompt says press enter to continue.\n\n• Working (4s • esc to interrupt)\n\n› " + composerHint + "\n\n  gpt-5.6-sol default fast · /repo/worktree",
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
			assert.Equal(t, tt.want, NewHarness(NewHarnessParams{Home: t.TempDir()}).ReadInput(tt.frame).Activity)
		})
	}
}

// TestReadActivityIgnoresTranscriptProse checks an agent discussing the markers is not read as
// being at one, only the bottom of the frame counting.
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
			assert.Equal(t, harness.ActivityIdle, NewHarness(NewHarnessParams{Home: t.TempDir()}).ReadInput(tt.frame).Activity,
				"prose must not read as a live prompt or as work")
		})
	}
}

// TestReadActivityStripsANSI covers a frame captured with colours, where the status line comes
// back shredded by SGR codes.
func TestReadActivityStripsANSI(t *testing.T) {
	frame := "\x1b[38;5;39m•\x1b[39m \x1b[1mWorking\x1b[0m \x1b[2m(7s • esc to interrupt)\x1b[0m"

	assert.Equal(t, harness.ActivityWorking, NewHarness(NewHarnessParams{Home: t.TempDir()}).ReadInput(frame).Activity)
}

// TestReadActivityWithoutComposer covers a pane whose Codex has not drawn its input box, where
// reading it as idle would let mail be typed at a pane that is not listening.
func TestReadActivityWithoutComposer(t *testing.T) {
	tests := []struct {
		name  string
		frame string
	}{
		{
			name:  "a transcript of sent messages with no composer under it",
			frame: "› tell me about tmux\n" + strings.Repeat("\n  a line of the answer", 20) + "\n\n• That is all.",
		},
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
			assert.Equal(t, harness.ActivityUnrecognized, NewHarness(NewHarnessParams{Home: t.TempDir()}).ReadInput(tt.frame).Activity)
		})
	}
}

// TestReadComposerEndingInAMenuHint covers a composer whose own lowest line reads like the hint
// under Codex's file-search menu, where taking that line for the menu would send the search for
// the composer above the box and read the transcript back as what the box holds.
func TestReadComposerEndingInAMenuHint(t *testing.T) {
	frame := "› an earlier message I already sent\n" +
		"\n" +
		"› paste of codex keys:\n" +
		"\n" +
		"  enter insert · esc close · arrows switch search modes"

	input := NewHarness(NewHarnessParams{Home: t.TempDir()}).ReadInput(frame)

	assert.Equal(t, harness.ActivityComposing, input.Activity)
	assert.Equal(t, []string{" paste of codex keys:", ""}, input.Held)
}
