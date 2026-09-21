package weavelint

import (
	"testing"

	"weavelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestNoMise covers the word in a comment, a string, an identifier and an environment
// variable, beside words that only contain the letters.
func TestNoMise(t *testing.T) {
	analyzer, err := newNoMise(config.NoMiseConfig{})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "nomise")
}

// TestNoMiseExclude covers a file the settings leave alone.
func TestNoMiseExclude(t *testing.T) {
	analyzer, err := newNoMise(config.NoMiseConfig{Exclude: []string{`nomiseskip/`}})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "nomiseskip")
}

// TestNoMiseSkip covers the rule turned off, which reports nothing.
func TestNoMiseSkip(t *testing.T) {
	analyzer, err := newNoMise(config.NoMiseConfig{Skip: true})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/quiet")
}
