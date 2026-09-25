package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestWrapPhrase covers a wrapping message that opens some other way, which is a finding, beside
// the two openings that are allowed and a message that wraps nothing.
func TestWrapPhrase(t *testing.T) {
	analyzer, err := newWrapPhrase(config.WrapPhraseConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "wrap")
}

// TestWrapPhraseExcluded covers a package the rule was configured not to hold for, whose
// messages open with whatever prefix they like.
func TestWrapPhraseExcluded(t *testing.T) {
	analyzer, err := newWrapPhrase(config.WrapPhraseConfig{Exclude: []string{"testdata/src/wrapkeyed/"}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "wrapkeyed")
}

// TestWrapPhraseSkip covers the rule turned off, which reports nothing anywhere.
func TestWrapPhraseSkip(t *testing.T) {
	analyzer, err := newWrapPhrase(config.WrapPhraseConfig{Skip: true})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "wrapkeyed")
}
