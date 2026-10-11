package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestViewSplit covers drawing outside a view file, names that only look like a view or a view file, a view writing its own copy, and a view writing through a pointer, map or slice.
func TestViewSplit(t *testing.T) {
	analyzer, err := newViewSplit(config.ViewSplitConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "viewsplit")
}
