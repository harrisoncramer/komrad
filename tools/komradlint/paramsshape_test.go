package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestParamsShape covers constructors, functions and methods in the right shape beside ones
// whose params are misnamed or sit elsewhere in the file.
func TestParamsShape(t *testing.T) {
	analyzer, err := newParamsShape(config.ParamsShapeConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "params")
}

// TestParamsShapeSkip covers the rule turned off, which reports nothing.
func TestParamsShapeSkip(t *testing.T) {
	analyzer, err := newParamsShape(config.ParamsShapeConfig{Skip: true})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "paramsskip")
}
