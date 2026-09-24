package komradelint

import (
	"testing"

	"komradelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestSingleAppend covers a plain mapping loop, one through a named function and one into a
// field, beside loops that index, filter, spread, copy, use the index, range over a map or a
// string, grow the ranged slice, append into the element, or do more than the one append.
func TestSingleAppend(t *testing.T) {
	analyzer, err := newSingleAppend(config.SingleAppendConfig{})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "mapping")
}

// TestSingleAppendExclude covers a file the settings leave alone.
func TestSingleAppendExclude(t *testing.T) {
	analyzer, err := newSingleAppend(config.SingleAppendConfig{Exclude: []string{`mappingskip/`}})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "mappingskip")
}

// TestSingleAppendSkip covers the rule turned off, which reports nothing.
func TestSingleAppendSkip(t *testing.T) {
	analyzer, err := newSingleAppend(config.SingleAppendConfig{Skip: true})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "mappingskip")
}
