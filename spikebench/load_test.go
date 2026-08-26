package spikebench

import (
	"context"
	"testing"
	"time"

	"weave/internal/diff"
	"weave/internal/review"
)

func TestLoadENG3518(t *testing.T) {
	dir := "/Users/harrisoncramer/.local/state/weave/worktrees/01a03ec1-a7a1-73a1-b0ae-2634fb28b3b5"

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	patch, err := diff.Load(ctx, diff.LoadParams{Dir: dir, Base: ""})
	t.Logf("diff.Load: %v err=%v", time.Since(start), err)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("files=%d", len(patch.Files))

	start = time.Now()
	view := review.NewView(patch, true)
	t.Logf("review.NewView: %v rows=%d", time.Since(start), view.Len())

	start = time.Now()
	for y := range view.Len() {
		_ = view.RenderRow(y, false, 160, 0)
	}
	t.Logf("render all rows: %v", time.Since(start))
}
