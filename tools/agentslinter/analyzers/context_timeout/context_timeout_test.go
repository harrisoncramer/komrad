package contexttimeout_test

import (
	"testing"

	contexttimeout "agentslinter/analyzers/context_timeout"
	"agentslinter/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

var testdata = analysistest.TestData()

// TestPanelContextsNeedANamedTimeout pins that an unbounded context, and a timeout written
// inline, are both found in a file the include patterns cover.
func TestPanelContextsNeedANamedTimeout(t *testing.T) {
	analyzer := contexttimeout.NewAnalyzer(config.ContextTimeoutConfig{Include: []string{`src/panel/`}})
	analysistest.Run(t, testdata, analyzer, "panel")
}

// TestFilesOutsideTheIncludeAreLeftAlone pins that the rule reaches only the paths it names,
// since a background context is the right answer nearly everywhere else.
func TestFilesOutsideTheIncludeAreLeftAlone(t *testing.T) {
	analyzer := contexttimeout.NewAnalyzer(config.ContextTimeoutConfig{Include: []string{`src/panel/`}})
	analysistest.Run(t, testdata, analyzer, "elsewhere")
}

// TestEmptyIncludeIsAnError pins that a rule left without a path to reach fails rather than
// reading as on while checking nothing.
func TestEmptyIncludeIsAnError(t *testing.T) {
	analyzer := contexttimeout.NewAnalyzer(config.ContextTimeoutConfig{})
	if _, err := analyzer.Run(nil); err == nil {
		t.Fatal("expected an empty include list to fail the analyzer")
	}
}

// TestBadIncludePatternIsAnError pins that an include pattern that will not compile fails the
// analyzer rather than silently matching nothing.
func TestBadIncludePatternIsAnError(t *testing.T) {
	analyzer := contexttimeout.NewAnalyzer(config.ContextTimeoutConfig{Include: []string{"("}})
	if _, err := analyzer.Run(nil); err == nil {
		t.Fatal("expected a bad include pattern to fail the analyzer")
	}
}
