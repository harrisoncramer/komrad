package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestHandlerShape covers a handler that hands its strict half to the adapter beside one that
// answers for itself.
func TestHandlerShape(t *testing.T) {
	analyzer, err := newHandlerShape(config.HandlerShapeConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "handlers")
}

// TestHandlerShapeExcluded covers a package the rule was configured not to hold for, such as the
// adapter itself, whose own signature is the one it checks for.
func TestHandlerShapeExcluded(t *testing.T) {
	analyzer, err := newHandlerShape(config.HandlerShapeConfig{Exclude: []string{"testdata/src/handlersskip/"}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "handlersskip")
}

// TestHandlerShapeSkip covers the rule turned off, which reports nothing.
func TestHandlerShapeSkip(t *testing.T) {
	analyzer, err := newHandlerShape(config.HandlerShapeConfig{Skip: true})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "handlersskip")
}
