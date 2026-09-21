package weavelint

import (
	"testing"

	"weavelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestConversionNameRender covers the service side, where a function returning a proto message
// under any name but render is a finding and a method, a reader or the render package is not.
func TestConversionNameRender(t *testing.T) {
	analyzer, err := newConversionName(config.ConversionNameConfig{Render: []string{"testdata/src/weave/internal/rpc/"}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/internal/rpc/conv", "weave/internal/rpc/render")
}

// TestConversionNameFrom covers the panel side, where protoToX, xFromProto and a render taking
// a proto message are findings and xFrom is not.
func TestConversionNameFrom(t *testing.T) {
	analyzer, err := newConversionName(config.ConversionNameConfig{From: []string{"testdata/src/weave/internal/ui/"}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/internal/ui")
}

// TestConversionNameSkip covers the rule turned off, which reports nothing.
func TestConversionNameSkip(t *testing.T) {
	analyzer, err := newConversionName(config.ConversionNameConfig{
		Skip:   true,
		Render: []string{"testdata/src/weave/quiet/"},
		From:   []string{"testdata/src/weave/quiet/"},
	})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/quiet")
}
