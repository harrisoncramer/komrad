package claude

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
)

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
		// Some panes draw Claude's bullet as U+25CF rather than U+23FA.
		{file: "filled_bullets_while_a_command_runs.txt", want: harness.ActivityWorking},
		{file: "filled_bullets_after_a_truncated_tool_call.txt", want: harness.ActivityIdle},
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

			assert.Equal(t, tt.want, NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).ReadInput(string(frame)).Activity)
		})
	}
}
