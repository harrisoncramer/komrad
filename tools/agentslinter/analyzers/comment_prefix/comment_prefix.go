// Package commentprefix contains the analyzer responsible for checking that any
// doc comment on an exported function or method begins with the identifier name.
package commentprefix

import (
	"fmt"
	"go/ast"
	"strings"
	"unicode"

	"agentslinter/config"

	"golang.org/x/tools/go/analysis"
)

// NewAnalyzer builds the comment-prefix analyzer. It does not require exported
// functions to be documented, but when a doc comment exists it must start with
// the identifier name so that an LSP rename keeps the comment in sync.
func NewAnalyzer(_ config.CommentPrefixConfig) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "commentprefix",
		Doc:  "Checks that doc comments on exported functions and methods begin with the identifier name",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				// Test files hold exported helpers (TestXxx, BenchmarkXxx) whose
				// comments don't describe a renamable API; skip them entirely.
				if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
					continue
				}
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || !fn.Name.IsExported() || fn.Doc == nil {
						continue
					}
					text := strings.TrimSpace(fn.Doc.Text())
					if text == "" {
						continue
					}
					name := fn.Name.Name
					if !startsWithWord(text, name) {
						pass.Report(analysis.Diagnostic{
							Pos:     fn.Doc.Pos(),
							Message: fmt.Sprintf("doc comment for exported %s %q must begin with %q", funcKind(fn), name, name),
						})
					}
				}
			}
			return nil, nil
		},
	}
}

// startsWithWord reports whether text begins with word as a whole word, i.e. word
// is not immediately followed by another identifier character.
func startsWithWord(text, word string) bool {
	if !strings.HasPrefix(text, word) {
		return false
	}
	rest := text[len(word):]
	if rest == "" {
		return true
	}
	r := rune(rest[0])
	return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}

// funcKind returns "method" when the declaration has a receiver, otherwise "function".
func funcKind(fn *ast.FuncDecl) string {
	if fn.Recv != nil {
		return "method"
	}
	return "function"
}
