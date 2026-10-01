package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestParamsShape covers constructors, plain functions and methods with their params in and out
// of place, and helpers that forward a struct named for another function.
func TestParamsShape(t *testing.T) {
	analyzer, err := newParamsShape(config.ParamsShapeConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "constructors", "params")
}

// TestParamsShapeSkip covers the rule turned off, which reports nothing.
func TestParamsShapeSkip(t *testing.T) {
	analyzer, err := newParamsShape(config.ParamsShapeConfig{Skip: true})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "paramsskip")
}
