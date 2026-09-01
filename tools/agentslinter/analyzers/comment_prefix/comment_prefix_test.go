package commentprefix_test

import (
	"testing"

	commentprefix "agentslinter/analyzers/comment_prefix"
	"agentslinter/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

var testdata = analysistest.TestData()

// TestInvalidCommentPrefix pins that a doc comment not starting with the identifier is found.
func TestInvalidCommentPrefix(t *testing.T) {
	analyzer := commentprefix.NewAnalyzer(config.CommentPrefixConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "invalid")
}

// TestValidCommentPrefix pins that a missing doc comment is left alone until require is set.
func TestValidCommentPrefix(t *testing.T) {
	analyzer := commentprefix.NewAnalyzer(config.CommentPrefixConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "valid")
}

// TestRequiredCommentPrefix pins that require asks for the comment exported code is missing.
func TestRequiredCommentPrefix(t *testing.T) {
	analyzer := commentprefix.NewAnalyzer(config.CommentPrefixConfig{Skip: false, Require: true})
	analysistest.Run(t, testdata, analyzer, "required")
}

// TestTestsCommentPrefix pins that a test file is held to its tests and not to a fake's stubs.
func TestTestsCommentPrefix(t *testing.T) {
	analyzer := commentprefix.NewAnalyzer(config.CommentPrefixConfig{Skip: false, Require: true})
	analysistest.Run(t, testdata, analyzer, "tests")
}
