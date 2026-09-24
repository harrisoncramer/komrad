package weavelint

import (
	"testing"

	"weavelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestFakeStore covers a test method matching a store's own by what it takes or returns, a service's method taking a store's type, an unexported helper, and the real store.
func TestFakeStore(t *testing.T) {
	analyzer, err := newFakeStore(config.FakeStoreConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/internal/fakestore")
}
