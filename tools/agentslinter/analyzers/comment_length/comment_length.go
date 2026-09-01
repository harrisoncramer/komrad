// Package commentlength holds every comment in weave to a single sentence, so a comment says
// the one thing it is for rather than narrating the code under it.
//
// The two exceptions are the ones where prose earns its place. A package comment may run as
// long as it needs to, since it is the only place a package gets to explain what it is for and
// it is what a reader meets first on pkg.go.dev. A free paragraph inside a file may run long
// behind an Explainer prefix, which marks it as reasoning set aside on purpose rather than a
// comment that got away from its author.
package commentlength

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"regexp"
	"strings"

	"agentslinter/config"

	"golang.org/x/tools/go/analysis"
)

const explainerPrefix = "Explainer:"

const (
	docTooLong         = "doc comment must be a single sentence"
	commentTooLong     = `comment must be a single sentence unless the paragraph starts with "Explainer:"`
	explainerOnDoc     = `"Explainer:" is for the free paragraphs that explain a file, not for a doc comment`
	explainerOnPackage = `a package comment is godoc output, so write the paragraph without the "Explainer:" prefix`
)

// commentKind is what a comment group is attached to, which decides how much room it gets.
type commentKind int

const (
	freeComment commentKind = iota
	declarationDoc
	packageDoc
)

// NewAnalyzer builds the comment-length analyzer, which holds every comment to one sentence
// unless it is a package comment or a free paragraph setting its reasoning aside behind the
// Explainer prefix.
func NewAnalyzer(cfg config.CommentLengthConfig) *analysis.Analyzer {
	excluded, compileErr := compileExcludes(cfg.Exclude)

	return &analysis.Analyzer{
		Name: "commentlength",
		Doc:  "Checks that comments are a single sentence unless they are prefixed with Explainer:",
		Run: func(pass *analysis.Pass) (any, error) {
			if compileErr != nil {
				return nil, compileErr
			}
			for _, file := range pass.Files {
				if isExcluded(excluded, pass.Fset.File(file.Pos()).Name()) {
					continue
				}
				docs := declarationDocs(file)
				for _, group := range file.Comments {
					if message, ok := complaint(group.Text(), kindOf(group, file, docs)); ok {
						pass.Report(analysis.Diagnostic{Pos: group.Pos(), Message: message})
					}
				}
			}
			return nil, nil
		},
	}
}

func complaint(text string, kind commentKind) (string, bool) {
	paragraphs := paragraphsOf(text)

	switch kind {
	case packageDoc:
		for _, paragraph := range paragraphs {
			if strings.HasPrefix(paragraph, explainerPrefix) {
				return explainerOnPackage, true
			}
		}

		return "", false
	case declarationDoc:
		for _, paragraph := range paragraphs {
			if strings.HasPrefix(paragraph, explainerPrefix) {
				return explainerOnDoc, true
			}
		}
		if countSentences(strings.Join(paragraphs, " ")) > 1 {
			return docTooLong, true
		}

		return "", false
	case freeComment:
		var counted []string
		for _, paragraph := range paragraphs {
			if !strings.HasPrefix(paragraph, explainerPrefix) {
				counted = append(counted, paragraph)
			}
		}
		if countSentences(strings.Join(counted, " ")) > 1 {
			return commentTooLong, true
		}

		return "", false
	}

	return "", false
}

// kindOf reports what a comment group is attached to, since the package comment is the one
// place in a file that is meant to run long.
func kindOf(group *ast.CommentGroup, file *ast.File, docs map[*ast.CommentGroup]bool) commentKind {
	switch {
	case group == file.Doc:
		return packageDoc
	case docs[group]:
		return declarationDoc
	default:
		return freeComment
	}
}

func paragraphsOf(text string) []string {
	var paragraphs []string
	for _, paragraph := range strings.Split(text, "\n\n") {
		if trimmed := strings.TrimSpace(paragraph); trimmed != "" {
			paragraphs = append(paragraphs, trimmed)
		}
	}

	return paragraphs
}

// declarationDocs collects the comment groups documenting a declaration, which leaves
// out the package doc that explains the file as a whole.
func declarationDocs(file *ast.File) map[*ast.CommentGroup]bool {
	docs := map[*ast.CommentGroup]bool{}
	mark := func(group *ast.CommentGroup) {
		if group != nil {
			docs[group] = true
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch decl := node.(type) {
		case *ast.Field:
			mark(decl.Doc)
		case *ast.ImportSpec:
			mark(decl.Doc)
		case *ast.ValueSpec:
			mark(decl.Doc)
		case *ast.TypeSpec:
			mark(decl.Doc)
		case *ast.GenDecl:
			mark(decl.Doc)
		case *ast.FuncDecl:
			mark(decl.Doc)
		}

		return true
	})

	return docs
}

func compileExcludes(patterns []string) ([]*regexp.Regexp, error) {
	var compiled []*regexp.Regexp
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("comment_length: exclude pattern %q: %w", pattern, err)
		}
		compiled = append(compiled, re)
	}

	return compiled, nil
}

func isExcluded(patterns []*regexp.Regexp, name string) bool {
	path := filepath.ToSlash(name)
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}

	return false
}

var abbreviations = map[string]bool{
	"e.g.":    true,
	"i.e.":    true,
	"etc.":    true,
	"vs.":     true,
	"cf.":     true,
	"resp.":   true,
	"approx.": true,
	"no.":     true,
	"mr.":     true,
	"ms.":     true,
	"dr.":     true,
	"al.":     true,
}

func countSentences(text string) int {
	sentences := 0
	trailing := false
	for _, field := range strings.Fields(text) {
		if endsSentence(field) {
			sentences++
			trailing = false
			continue
		}
		trailing = true
	}
	if trailing {
		sentences++
	}

	return sentences
}

func endsSentence(field string) bool {
	if strings.Contains(field, "://") {
		return false
	}
	word := strings.TrimRight(field, `)]}"'`+"`")
	if strings.HasSuffix(word, "...") {
		return false
	}
	if !strings.HasSuffix(word, ".") && !strings.HasSuffix(word, "!") && !strings.HasSuffix(word, "?") {
		return false
	}

	return !abbreviations[strings.ToLower(word)]
}
