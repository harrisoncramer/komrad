package weavelint

import (
	"go/parser"
	"go/token"
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

// TestFindClauseLandsOnTheWord pins the column for a line comment and a block comment alike.
func TestFindClauseLandsOnTheWord(t *testing.T) {
	for _, src := range []string{
		"package p\n\n// a bounds the look, since a mount can hang.\nconst a = 1\n",
		"package p\n\n/* a bounds the look, since a mount can hang. */\nconst a = 1\n",
		"package p\n\n// a bounds the look,\n// since a mount can hang.\nconst a = 1\n",
	} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}

		clause, pos, ok := findClause(file.Comments[0])
		if !ok || clause != ", since " {
			t.Fatalf("found %q, %v", clause, ok)
		}

		offset := fset.Position(pos).Offset
		if got := src[offset : offset+5]; got != "since" {
			t.Errorf("%q: landed on %q", src, got)
		}
	}
}
