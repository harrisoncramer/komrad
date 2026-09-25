package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestConstructorShape covers a constructor in the right shape beside one whose params are
// misnamed, one whose type sits above its params and one whose params sit elsewhere.
func TestConstructorShape(t *testing.T) {
	analyzer, err := newConstructorShape(config.ConstructorShapeConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "constructors")
}

// TestConstructorShapeSkip covers the rule turned off, which reports nothing.
func TestConstructorShapeSkip(t *testing.T) {
	analyzer, err := newConstructorShape(config.ConstructorShapeConfig{Skip: true})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "constructorsskip")
}
