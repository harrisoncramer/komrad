package commentlength_test

import (
	"testing"

	commentlength "agentslinter/analyzers/comment_length"
	"agentslinter/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

var testdata = analysistest.TestData()

// TestInvalidCommentLength pins that a comment running past one sentence is found.
func TestInvalidCommentLength(t *testing.T) {
	analyzer := commentlength.NewAnalyzer(config.CommentLengthConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "invalid")
}

// TestValidCommentLength pins that a one-sentence comment, and a paragraph set aside as an
// explainer, are left alone.
func TestValidCommentLength(t *testing.T) {
	analyzer := commentlength.NewAnalyzer(config.CommentLengthConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "valid")
}

// TestExcludedFileIsNotReported pins that a file matching an exclude pattern is skipped
// whole.
func TestExcludedFileIsNotReported(t *testing.T) {
	analyzer := commentlength.NewAnalyzer(config.CommentLengthConfig{Exclude: []string{`excluded/excluded\.go$`}})
	analysistest.Run(t, testdata, analyzer, "excluded")
}

// TestExcludeDoesNotReachOtherFiles pins that an exclude pattern matching nothing leaves
// every other file checked.
func TestExcludeDoesNotReachOtherFiles(t *testing.T) {
	analyzer := commentlength.NewAnalyzer(config.CommentLengthConfig{Exclude: []string{`nothing/here\.go$`}})
	analysistest.Run(t, testdata, analyzer, "invalid")
}

// TestBadExcludePatternIsAnError pins that an exclude pattern that will not compile fails the
// analyzer rather than silently matching nothing.
func TestBadExcludePatternIsAnError(t *testing.T) {
	analyzer := commentlength.NewAnalyzer(config.CommentLengthConfig{Exclude: []string{"("}})
	if _, err := analyzer.Run(nil); err == nil {
		t.Fatal("expected a bad exclude pattern to fail the analyzer")
	}
}
