package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPushDescriptionSaysWhatWillHappen covers the push description naming what goes up, what is overwritten and what stays behind.
func TestPushDescriptionSaysWhatWillHappen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		plan     pushPlan
		want     []string
		wantNone []string
	}{
		{
			name: "a plain push overwrites nothing",
			plan: pushPlan{
				branch:  "agent/a1",
				commits: 1,
			},
			want:     []string{"1 commit will go up to origin as agent/a1."},
			wantNone: []string{"overwritten", "stay behind", "Auto-merge"},
		},
		{
			name: "a forced push says origin's copy is overwritten",
			plan: pushPlan{
				branch:  "agent/a1",
				commits: 3,
				forced:  true,
			},
			want: []string{"3 commits will go up", "will be overwritten", "refused if origin has moved"},
		},
		{
			name: "uncommitted files are named as staying behind",
			plan: pushPlan{
				branch:      "agent/a1",
				commits:     2,
				uncommitted: 4,
			},
			want: []string{"4 files nothing has committed will stay behind"},
		},
		{
			name: "auto-merge on says what goes up may land",
			plan: pushPlan{
				branch:    "agent/a1",
				commits:   1,
				autoMerge: true,
			},
			want: []string{"Auto-merge is on", "merges by itself"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			description := pushDescription(tt.plan)
			for _, want := range tt.want {
				assert.Contains(t, description, want)
			}
			for _, none := range tt.wantNone {
				assert.NotContains(t, description, none)
			}
		})
	}
}
