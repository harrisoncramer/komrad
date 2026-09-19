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
		held []string
	}{
		{
			file: "dim_rule_above_an_empty_composer.txt",
			want: harness.ActivityIdle,
			held: []string{" ", ""},
		},
		{
			file: "dim_rule_above_a_composer_holding_a_draft.txt",
			want: harness.ActivityComposing,
			held: []string{
				" a codex draft that nobody sent, long enough to wrap across the composer more",
				"  than once so the frame shows it",
				"",
			},
		},
		{
			file: "composer_holding_a_draft_across_a_blank_line.txt",
			want: harness.ActivityComposing,
			held: []string{" first line", "", "  third line after a blank", ""},
		},
		{
			file: "slash_menu_under_the_composer.txt",
			want: harness.ActivityComposing,
			held: []string{" /"},
		},
		{
			file: "file_search_menu_under_the_composer.txt",
			want: harness.ActivityComposing,
			held: []string{" please look at @"},
		},
		{
			file: "file_search_menu_under_a_draft_across_a_blank_line.txt",
			want: harness.ActivityComposing,
			held: []string{" first line", "", "  third line after a blank @"},
		},
		{
			file: "file_search_menu_matching_nothing.txt",
			want: harness.ActivityComposing,
			held: []string{" please look at @zzzznomatch"},
		},
		{
			file: "composer_holding_a_draft_reading_like_a_menu_hint.txt",
			want: harness.ActivityComposing,
			held: []string{
				" first line",
				"",
				"  second line",
				"  enter insert · esc close · arrows switch search modes",
				"",
			},
		},
		{
			file: "starting_mcp_servers_offering_interrupt.txt",
			want: harness.ActivityWorking,
		},
		{
			file: "api_error_above_composer.txt",
			want: harness.ActivityUnrecognized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			frame, err := os.ReadFile(filepath.Join("testdata", tt.file))
			if err != nil {
				t.Fatal(err)
			}

			input := NewHarness(NewHarnessParams{Home: t.TempDir()}).ReadInput(string(frame))

			assert.Equal(t, tt.want, input.Activity)
			assert.Equal(t, tt.held, input.Held)
		})
	}
}
