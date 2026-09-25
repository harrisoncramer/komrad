package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestSentinelKind covers a package the rule holds for, where a sentinel built by errors.New is
// a finding and one built by a fail constructor is not.
func TestSentinelKind(t *testing.T) {
	analyzer, err := newSentinelKind(config.SentinelKindConfig{})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "domain")
}

// TestSentinelKindExcluded covers a package the rule was configured not to hold for, whose
// sentinels are left alone.
func TestSentinelKindExcluded(t *testing.T) {
	analyzer, err := newSentinelKind(config.SentinelKindConfig{Exclude: []string{"testdata/src/panelside/"}})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "panelside")
}

// TestSentinelKindSkip covers the rule turned off, which reports nothing anywhere.
func TestSentinelKindSkip(t *testing.T) {
	analyzer, err := newSentinelKind(config.SentinelKindConfig{Skip: true})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "panelside")
}
