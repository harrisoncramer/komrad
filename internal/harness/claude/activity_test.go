package claude

import (
	"os"
	"path/filepath"
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
			name:  "the box in shell mode, which draws a marker of its own",
			frame: "───────────────\n\x1b[39m! aws sso login\n───────────────\n  -- INSERT -- ! for shell mode",
			want:  harness.ActivityComposing,
		},
		{
			name:  "an empty box in shell mode",
			frame: "───────────────\n\x1b[39m!\n───────────────\n  -- INSERT -- ! for shell mode",
			want:  harness.ActivityIdle,
		},
		{
			name:  "a title drawn on the rule opening the box",
			frame: "─────── a conversation title ─\n\x1b[39m❯ half a question about\n───────────────\n  -- INSERT --",
			want:  harness.ActivityComposing,
		},
		{
			name:  "a queued-message divider drawn as a titled rule above a question",
			frame: "⏺ Some earlier reply\n\n──────── 1 new message ────────\n❯ the user's queued message\n\n╭─ Do you want to proceed? ─╮\n│ ❯ 1. Yes                  │\n╰───────────────────────────╯\n  esc to cancel",
			want:  harness.ActivityWaiting,
		},
		{
			name:  "a bang command echoed into the transcript under a rule",
			frame: "⏺ Here is the command:\n\n────────────────────────────\n!important note in the reply\n\n✳ Honking… (12s · 1.2k tokens)",
			want:  harness.ActivityWorking,
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
		{
			name:  "a question whose own preview has pushed its cursor far up the pane",
			frame: "Which shape?\n\n❯ 1. Chronological\n  2. Threads as rows\n" + strings.Repeat("                    │ a preview row │\n", 35),
			want:  harness.ActivityWaiting,
		},
		{
			name:  "a bordered confirmation draws its cursor inside its own frame",
			frame: "Do you want to proceed?\n│ ❯ 1. Yes\n│   2. No\n" + strings.Repeat("│ padding\n", 30),
			want:  harness.ActivityWaiting,
		},
		{
			name: "a bordered question under a status line the turn before it left on screen",
			frame: "⏺ Bash(rm -rf build)\n  ⎿  running\n\n" +
				"✻ Reticulating… (12s · ↓ 1.2k tokens)\n" +
				"╭──────────────────────────╮\n" +
				"│ Bash command             │\n" +
				"│ Do you want to proceed?  │\n" +
				"│ ❯ 1. Yes                 │\n" +
				"│   2. No                  │\n" +
				"╰──────────────────────────╯\n" +
				"Enter to select · Esc to cancel",
			want: harness.ActivityWaiting,
		},
		{
			name:  "a dim run left open above the box does not lose the box",
			frame: "\x1b[2mI opened a dim run and never closed it\n" + strings.Repeat("  more transcript\n", 9) + idleFrame,
			want:  harness.ActivityIdle,
		},
		{
			name:  "a status line above a rule, with no box drawn under it",
			frame: "✻ Compacting conversation… (30s)\n───────────────\n  Ctrl+C to stop",
			want:  harness.ActivityWorking,
		},
		{
			name:  "a hook named inside the timer",
			frame: "· Canoodling… (running PostToolUse hook · 3m 12s · ↓ 5.1k tokens)",
			want:  harness.ActivityWorking,
		},
		{
			name:  "the box saying a message is queued, with the queue scrolled off",
			frame: "  the tail of some reply\n───────────────\n❯ \x1b[2mPress up to edit queued messages\x1b[0m\n───────────────\n  -- INSERT --",
			want:  harness.ActivityWorking,
		},
		{
			name:  "a marker greyed in a colour other than the rule's",
			frame: "\x1b[38;5;244m───────────────\n\x1b[38;5;246m❯ \x1b[39m\n\x1b[38;5;244m───────────────\n  -- INSERT --",
			want:  harness.ActivityWorking,
		},
		{
			name:  "a marker in the terminal's own colour under a coloured rule",
			frame: "\x1b[38;5;244m───────────────\n\x1b[39m❯ \n\x1b[38;5;244m───────────────\n  -- INSERT --",
			want:  harness.ActivityIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).ReadInput(tt.frame).Activity)
		})
	}
}

// TestReadActivityIgnoresTranscriptProse checks transcript text carrying a marker phrase is not
// read as a live prompt, however close to the box it sits.
func TestReadActivityIgnoresTranscriptProse(t *testing.T) {
	tests := []struct {
		name  string
		frame string
	}{
		{
			name:  "prose directly above the box",
			frame: "⏺ I match `Enter to select` and `Do you want`, and the timer\n  `· Cooking… (1m 50s)`.\n" + idleFrame,
		},
		{
			name:  "an answer ending in a truncated path",
			frame: "⏺ Spawned it at ~/.local/state/weave/worktrees/01a069a0-… — not a child.\n" + idleFrame,
		},
		{
			name:  "an answer quoting a spinner, wrapped",
			frame: "  Live spinner sits above the box and carries a timer (✶ Choreographing… (2m\n  41s · ↓ 8.8k tokens)).\n" + idleFrame,
		},
		{
			name:  "waiting phrases in prose rather than in a footer",
			frame: `I split it so "esc to cancel" and "Enter to select" in a message do not read as a prompt.` + "\n\n\n\n\n\n\n\n\n\n───────\n❯ \n───────\n  -- INSERT --",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, harness.ActivityIdle, NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).ReadInput(tt.frame).Activity,
				"prose must not read as a live prompt or as work")
		})
	}
}

// TestReadActivityOnCapturedFrames runs the classifier over frames taken out of real panes.
func TestReadActivityOnCapturedFrames(t *testing.T) {
	tests := []struct {
		file string
		want harness.Activity
	}{
		{file: "spinner_above_a_tip.txt", want: harness.ActivityWorking},
		{file: "answer_with_truncated_path_and_a_draft.txt", want: harness.ActivityComposing},
		{file: "answer_quoting_a_spinner.txt", want: harness.ActivityIdle},
		{file: "question_with_its_footer.txt", want: harness.ActivityWaiting},
		{file: "question_whose_footer_a_preview_pushed_off.txt", want: harness.ActivityWaiting},
		{file: "question_whose_cursor_a_preview_pushed_off.txt", want: harness.ActivityWaiting},
		{file: "streaming_reply_hides_the_status_line.txt", want: harness.ActivityWorking},
		{file: "scrolled_away_from_a_live_turn.txt", want: harness.ActivityWorking},
		{file: "scrolled_away_after_the_turn_ended.txt", want: harness.ActivityIdle},
		{file: "panel_whose_footer_the_pane_cut_off.txt", want: harness.ActivityWaiting},
		// Claude 2.1.278 closes the plan picker with the plan's path rather than an Enter to select footer.
		{file: "a_plan_picker_under_its_path_footer.txt", want: harness.ActivityWaiting},
		{file: "api_retry_banner.txt", want: harness.ActivityWorking},
		{file: "api_error_ended_the_turn.txt", want: harness.ActivityIdle},
		{file: "a_title_on_the_box_above_a_spinner.txt", want: harness.ActivityWorking},
		{file: "a_title_on_the_box_over_a_greyed_prompt.txt", want: harness.ActivityWorking},
		{file: "a_title_on_the_box_over_an_empty_prompt.txt", want: harness.ActivityIdle},
		{file: "a_recalled_history_entry_in_the_box.txt", want: harness.ActivityComposing},
		{file: "a_shell_command_typed_into_the_box.txt", want: harness.ActivityComposing},
		// Claude 2.1.277 draws a queued message flush with the margin, under the status line and
		// over a box whose greyed marker no longer shares the rule's colour.
		{file: "a_message_queued_under_a_compaction.txt", want: harness.ActivityWorking},
		{file: "a_hook_note_inside_the_timer.txt", want: harness.ActivityWorking},
		{file: "a_prompt_greyed_lighter_than_its_rule.txt", want: harness.ActivityWorking},
		{file: "an_empty_prompt_under_the_new_greys.txt", want: harness.ActivityIdle},
		{file: "a_suggestion_in_the_box_after_the_turn_ended.txt", want: harness.ActivityIdle},
		// Some panes draw Claude's bullet as U+25CF rather than U+23FA.
		{file: "filled_bullets_while_a_command_runs.txt", want: harness.ActivityWorking},
		{file: "filled_bullets_after_a_truncated_tool_call.txt", want: harness.ActivityIdle},
		{file: "a_lead_at_its_prompt_over_a_background_agent.txt", want: harness.ActivityWorking},
		{file: "a_lead_at_its_prompt_over_two_background_agents.txt", want: harness.ActivityWorking},
		{file: "a_finished_agent_under_its_waiting_line.txt", want: harness.ActivityIdle},
		{file: "a_screen_no_pattern_matches.txt", want: harness.ActivityUnrecognized},
		// Every agent draws this for a few seconds after the panel restarts, so the panel
		// showing a wave of uncertain rows on startup is this frame rather than a fault.
		{file: "a_pane_the_cli_has_not_drawn_yet.txt", want: harness.ActivityUnrecognized},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			frame, err := os.ReadFile(filepath.Join("testdata", tt.file))
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, tt.want, NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).ReadInput(string(frame)).Activity)
		})
	}
}
