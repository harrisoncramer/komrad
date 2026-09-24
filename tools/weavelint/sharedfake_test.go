package weavelint

import (
	"testing"

	"weavelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestSharedFake covers a test type satisfying a boundary with a shared fake, one satisfying half of it, and the real implementation.
func TestSharedFake(t *testing.T) {
	analyzer, err := newSharedFake(config.SharedFakeConfig{
		Fakes: []config.SharedFake{{Interface: "sharedfake.paneStarter", Fake: "tmuxtest.Fake"}},
	})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "sharedfake")
}
