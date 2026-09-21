package weavelint

import (
	"go/ast"
	"go/token"
	"strings"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const commentClauseDoc = "Checks that a comment carries no trailing justification clause, " +
	"so a comment says what a thing is and leaves why to the commit message."

// clauses is every joint a justification hangs off a comment by.
var clauses = []string{", since ", ", because ", ", so that ", ", rather than ", ", which is why "}

// newCommentClause builds the rule that a comment does not fold in why the thing is that way.
func newCommentClause(settings config.CommentClauseConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "comment_clause",
		Doc:  commentClauseDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isUnder(pass, file, excluded) {
					continue
				}

				reportCommentClauses(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportCommentClauses reports every comment group in a file that carries one of the clauses.
func reportCommentClauses(pass *analysis.Pass, file *ast.File) {
	for _, group := range file.Comments {
		clause, pos, ok := findClause(group)
		if !ok {
			continue
		}

		pass.Reportf(pos, "comment justifies itself with %q: delete the clause, or say it in a sentence of its own", strings.TrimSpace(clause))
	}
}

// findClause returns the first clause a comment group carries and where its word starts.
func findClause(group *ast.CommentGroup) (string, token.Pos, bool) {
	var joined strings.Builder
	starts := make([]int, 0, len(group.List))
	for _, comment := range group.List {
		joined.WriteByte(' ')
		starts = append(starts, joined.Len())
		joined.WriteString(strings.ReplaceAll(commentBody(comment.Text), "\n", " "))
	}

	text := joined.String()
	for _, clause := range clauses {
		index := strings.Index(text, clause)
		if index < 0 {
			continue
		}

		return clause, clausePos(group, starts, index+2), true
	}

	return "", token.NoPos, false
}

// clausePos maps an offset into the joined text of a group back to a position in the file.
func clausePos(group *ast.CommentGroup, starts []int, offset int) token.Pos {
	for i := len(starts) - 1; i >= 0; i-- {
		if offset >= starts[i] {
			comment := group.List[i]
			marker := len(comment.Text) - len(commentBody(comment.Text))

			return comment.Pos() + token.Pos(marker+offset-starts[i])
		}
	}

	return group.Pos()
}

// commentBody is a comment's text with its opening marker gone.
func commentBody(text string) string {
	if strings.HasPrefix(text, "//") {
		return strings.TrimPrefix(text[2:], " ")
	}

	return strings.TrimSuffix(text[2:], "*/")
}
