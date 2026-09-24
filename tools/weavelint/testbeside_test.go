package weavelint

import (
	"testing"

	"weavelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestTestBeside covers a test file beside its source, an internal one beside it, a test file with no source, and an excluded one.
func TestTestBeside(t *testing.T) {
	analyzer, err := newTestBeside(config.BesideConfig{Exclude: []string{"export_test\\.go$"}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "beside")
}
