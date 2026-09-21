package weavelint

import (
	"testing"

	"weavelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCommentClause covers each clause on a doc comment, one split across two lines, one in a
// block comment and one inside a body, beside comments that carry the words without the joint.
func TestCommentClause(t *testing.T) {
	analyzer, err := newCommentClause(config.CommentClauseConfig{})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "clause")
}

// TestCommentClauseExclude covers a file the settings leave alone.
func TestCommentClauseExclude(t *testing.T) {
	analyzer, err := newCommentClause(config.CommentClauseConfig{Exclude: []string{`clauseskip/`}})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "clauseskip")
}

// TestCommentClauseSkip covers the rule turned off, which reports nothing.
func TestCommentClauseSkip(t *testing.T) {
	analyzer, err := newCommentClause(config.CommentClauseConfig{Skip: true})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/quiet")
}
