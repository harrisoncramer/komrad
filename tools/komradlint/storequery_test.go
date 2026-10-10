package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestStoreQuery covers a handler calling a query or building a store, a handler using a store's row type and pure helper, a test file, and an allowed service.
func TestStoreQuery(t *testing.T) {
	analyzer, err := newStoreQuery(config.StoreQueryConfig{Allow: []string{`^komrad/internal/notes$`}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrad/internal/rpc/notes", "komrad/internal/notes")
}
