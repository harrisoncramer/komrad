package weavelint

import (
	"testing"

	"weavelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestSentinelInline covers a fail constructor called inside a function, which is a finding,
// beside the package-level sentinel and the Saying on it, which are not.
func TestSentinelInline(t *testing.T) {
	analyzer, err := newSentinelInline(config.SentinelInlineConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "inline")
}
