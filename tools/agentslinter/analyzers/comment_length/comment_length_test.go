package commentlength_test

import (
	"testing"

	commentlength "agentslinter/analyzers/comment_length"
	"agentslinter/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

var testdata = analysistest.TestData()

func TestInvalidCommentLength(t *testing.T) {
	analyzer := commentlength.NewAnalyzer(config.CommentLengthConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "invalid")
}

func TestValidCommentLength(t *testing.T) {
	analyzer := commentlength.NewAnalyzer(config.CommentLengthConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "valid")
}

func TestExcludedFileIsNotReported(t *testing.T) {
	analyzer := commentlength.NewAnalyzer(config.CommentLengthConfig{Exclude: []string{`excluded/excluded\.go$`}})
	analysistest.Run(t, testdata, analyzer, "excluded")
}

func TestExcludeDoesNotReachOtherFiles(t *testing.T) {
	analyzer := commentlength.NewAnalyzer(config.CommentLengthConfig{Exclude: []string{`nothing/here\.go$`}})
	analysistest.Run(t, testdata, analyzer, "invalid")
}

func TestBadExcludePatternIsAnError(t *testing.T) {
	analyzer := commentlength.NewAnalyzer(config.CommentLengthConfig{Exclude: []string{"("}})
	if _, err := analyzer.Run(nil); err == nil {
		t.Fatal("expected a bad exclude pattern to fail the analyzer")
	}
}
