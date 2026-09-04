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
		{file: "api_retry_banner.txt", want: harness.ActivityWorking},
		{file: "api_error_ended_the_turn.txt", want: harness.ActivityIdle},
		{file: "a_pager_took_the_screen.txt", want: harness.ActivityUnrecognized},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			frame, err := os.ReadFile(filepath.Join("testdata", tt.file))
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, tt.want, NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).ReadActivity(string(frame)))
		})
	}
}
