package komradelint

import (
	"testing"

	"komradelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestBareHTTP covers the four spellings of an unconfigured client, and a client handed in
// being fine.
func TestBareHTTP(t *testing.T) {
	analyzer, err := newBareHTTP(config.BareHTTPConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrade/bare")
}

// TestBareHTTPExcluded covers the package allowed to build the client.
func TestBareHTTPExcluded(t *testing.T) {
	analyzer, err := newBareHTTP(config.BareHTTPConfig{Exclude: []string{"testdata/src/komrade/quiet/"}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrade/quiet")
}
