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
