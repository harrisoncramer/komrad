package komradelint

import (
	"testing"

	"komradelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestBlackBox covers a test inside the package driving a held type, a helper test beside it, and the export file allowed to see inside.
func TestBlackBox(t *testing.T) {
	analyzer, err := newBlackBox(config.BlackBoxConfig{
		Types:   []string{"blackbox.model"},
		Exclude: []string{"export_test\\.go$"},
	})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "blackbox")
}
