package commentprefix_test

import (
	"testing"

	commentprefix "agentslinter/analyzers/comment_prefix"
	"agentslinter/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

var testdata = analysistest.TestData()

func TestInvalidCommentPrefix(t *testing.T) {
	analyzer := commentprefix.NewAnalyzer(config.CommentPrefixConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "invalid")
}

func TestValidCommentPrefix(t *testing.T) {
	analyzer := commentprefix.NewAnalyzer(config.CommentPrefixConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "valid")
}
