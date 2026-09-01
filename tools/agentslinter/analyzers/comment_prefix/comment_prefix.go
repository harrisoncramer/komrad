// Package commentprefix holds a doc comment to starting with the name it documents, so renaming
// an identifier without touching its comment shows up as a lint failure rather than as a
// comment naming something that no longer exists.
package commentprefix

import (
	"fmt"
	"go/ast"
	"strings"
	"unicode"
	"unicode/utf8"

	"agentslinter/config"

	"golang.org/x/tools/go/analysis"
)

// NewAnalyzer builds the comment-prefix analyzer, which holds a doc comment to starting with
// the identifier name so a rename keeps the two in sync, and with require set asks for one.
func NewAnalyzer(cfg config.CommentPrefixConfig) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "commentprefix",
		Doc:  "Checks that doc comments on exported functions, methods and tests begin with the identifier name",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				isTest := strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go")
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || !isChecked(fn, isTest) {
						continue
					}
					check(pass, fn, isTest, cfg.Require)
				}
			}

			return nil, nil
		},
	}
}

func check(pass *analysis.Pass, fn *ast.FuncDecl, isTest, require bool) {
	name := fn.Name.Name
	text := ""
	if fn.Doc != nil {
		text = strings.TrimSpace(fn.Doc.Text())
	}

	if text == "" {
		if require && isRequired(fn, isTest) {
			pass.Report(analysis.Diagnostic{
				Pos:     fn.Pos(),
				Message: fmt.Sprintf("%s %q needs a doc comment beginning with %q", funcKind(fn, isTest), name, name),
			})
		}

		return
	}

	if !doesStartWithWord(text, name) {
		pass.Report(analysis.Diagnostic{
			Pos:     fn.Doc.Pos(),
			Message: fmt.Sprintf("doc comment for %s %q must begin with %q", funcKind(fn, isTest), name, name),
		})
	}
}

// isChecked reports whether a declaration is one the rule looks at, which in a test file is the
// tests themselves rather than the exported stubs a fake needs to satisfy an interface.
func isChecked(fn *ast.FuncDecl, isTest bool) bool {
	if !fn.Name.IsExported() {
		return false
	}

	if isTest {
		return fn.Recv == nil && isTestName(fn.Name.Name)
	}

	return true
}

// isRequired reports whether a missing doc comment is a finding, which a method on an
// unexported type is not, since it is not part of what the package offers.
func isRequired(fn *ast.FuncDecl, isTest bool) bool {
	if isTest || fn.Recv == nil {
		return true
	}

	return isExportedReceiver(fn.Recv)
}

func isExportedReceiver(recv *ast.FieldList) bool {
	if len(recv.List) == 0 {
		return false
	}

	expr := recv.List[0].Type
	for {
		switch t := expr.(type) {
		case *ast.StarExpr:
			expr = t.X
		case *ast.IndexExpr:
			expr = t.X
		case *ast.IndexListExpr:
			expr = t.X
		case *ast.Ident:
			return t.IsExported()
		default:
			return false
		}
	}
}

// isTestName follows go's own rule, where the prefix is a test only when what follows it is
// not a lower-case letter, so a helper named Testing is left alone.
func isTestName(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		rest, found := strings.CutPrefix(name, prefix)
		if !found {
			continue
		}
		if rest == "" {
			return true
		}
		first, _ := utf8.DecodeRuneInString(rest)

		return !unicode.IsLower(first)
	}

	return false
}

func doesStartWithWord(text, word string) bool {
	if !strings.HasPrefix(text, word) {
		return false
	}
	rest := text[len(word):]
	if rest == "" {
		return true
	}
	r := rune(rest[0])

	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
}

func funcKind(fn *ast.FuncDecl, isTest bool) string {
	switch {
	case isTest:
		return "test"
	case fn.Recv != nil:
		return "exported method"
	default:
		return "exported function"
	}
}
