package codex

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
		{file: "dim_rule_above_an_empty_composer.txt", want: harness.ActivityIdle},
		{file: "dim_rule_above_a_composer_holding_a_draft.txt", want: harness.ActivityComposing},
		{file: "slash_menu_under_the_composer.txt", want: harness.ActivityComposing},
		{file: "starting_mcp_servers_offering_interrupt.txt", want: harness.ActivityWorking},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			frame, err := os.ReadFile(filepath.Join("testdata", tt.file))
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, tt.want, NewHarness(NewHarnessParams{SessionsDir: t.TempDir()}).ReadActivity(string(frame)))
		})
	}
}
