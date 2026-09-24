package komradelint

import (
	"testing"

	"komradelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestConversionNameRender covers the service side, where a function returning a proto message
// under any name but render is a finding and a method, a reader or the render package is not.
func TestConversionNameRender(t *testing.T) {
	analyzer, err := newConversionName(config.ConversionNameConfig{Render: []string{"testdata/src/komrade/internal/rpc/"}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrade/internal/rpc/conv", "komrade/internal/rpc/render")
}

// TestConversionNameFrom covers the panel side, where protoToX, xFromProto and a render taking
// a proto message are findings and xFrom is not.
func TestConversionNameFrom(t *testing.T) {
	analyzer, err := newConversionName(config.ConversionNameConfig{From: []string{"testdata/src/komrade/internal/ui/"}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrade/internal/ui")
}

// TestConversionNameSkip covers the rule turned off, which reports nothing.
func TestConversionNameSkip(t *testing.T) {
	analyzer, err := newConversionName(config.ConversionNameConfig{
		Skip:   true,
		Render: []string{"testdata/src/komrade/quiet/"},
		From:   []string{"testdata/src/komrade/quiet/"},
	})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrade/quiet")
}
