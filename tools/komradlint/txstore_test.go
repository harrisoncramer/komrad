package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestTxStore covers stores built over the transaction, a store or service from outside it, a store built over another transaction, an event handler handed one, and a nested transaction.
func TestTxStore(t *testing.T) {
	analyzer, err := newTxStore(config.TxStoreConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrad/internal/ledger")
}
