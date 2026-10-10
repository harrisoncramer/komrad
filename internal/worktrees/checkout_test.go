package worktrees

import (
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"

	"github.com/stretchr/testify/assert"
)

// TestCheckoutReaderReadStandingPredictsOncePerHeadAndTip covers a reading reusing the last
// prediction while neither the head nor the base tip has moved.
func TestCheckoutReaderReadStandingPredictsOncePerHeadAndTip(t *testing.T) {
	tests := []struct {
		name          string
		firstHead     string
		secondHead    string
		secondTip     string
		wantPredicted int
	}{
		{
			name:          "a reading without a head predicts every time",
			firstHead:     "",
			secondHead:    "",
			secondTip:     "tip-one",
			wantPredicted: 2,
		},
		{
			name:          "a moved base tip predicts again",
			firstHead:     "head-one",
			secondHead:    "head-one",
			secondTip:     "tip-two",
			wantPredicted: 2,
		},
		{
			name:          "a moved head predicts again",
			firstHead:     "head-one",
			secondHead:    "head-two",
			secondTip:     "tip-one",
			wantPredicted: 2,
		},
		{
			name:          "a held head and tip reuse the prediction",
			firstHead:     "head-one",
			secondHead:    "head-one",
			secondTip:     "tip-one",
			wantPredicted: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			trees := &gittest.Trees{}
			reader := NewCheckoutReader(NewCheckoutReaderParams{Git: trees})

			reader.ReadStanding(t.Context(), ReadStandingParams{
				Dir:      dir,
				Readable: true,
				Base:     &git.Base{Ref: "origin/main", Tip: "tip-one"},
				HeadSHA:  tt.firstHead,
			})
			reader.ReadStanding(t.Context(), ReadStandingParams{
				Dir:      dir,
				Readable: true,
				Base:     &git.Base{Ref: "origin/main", Tip: tt.secondTip},
				HeadSHA:  tt.secondHead,
			})

			assert.Equal(t, tt.wantPredicted, trees.Predicted)
		})
	}
}
