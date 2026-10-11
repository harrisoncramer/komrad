package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestOneService covers a strict half reaching one service, one reading its row back, one stamping it from a clock, two services reached directly or through a helper, and a method that is not a strict half.
func TestOneService(t *testing.T) {
	analyzer, err := newOneService(config.OneServiceConfig{Readers: []string{"rows"}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrad/internal/rpc/billing")
}
