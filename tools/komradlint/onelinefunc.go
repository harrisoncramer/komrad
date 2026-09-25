package komradlint

import (
	"go/ast"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const onelineFuncDoc = "Checks that a function declaration with a body spans more than one line, " +
	"so every declared function reads the same way whatever its size."

// newOnelineFunc builds the rule that a declared function's braces sit on different lines.
func newOnelineFunc(settings config.OnelineFuncConfig) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "oneline_func",
		Doc:  onelineFuncDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) {
					continue
				}

				reportOnelineFuncs(pass, file)
			}

			return nil, nil
		},
	}
}

// reportOnelineFuncs reports every declared function whose braces share a line around a body.
func reportOnelineFuncs(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Body == nil || len(function.Body.List) == 0 {
			continue
		}

		opened := pass.Fset.Position(function.Body.Lbrace).Line
		closed := pass.Fset.Position(function.Body.Rbrace).Line
		if opened != closed {
			continue
		}

		pass.Reportf(function.Pos(), "%s is declared on one line: put its body on lines of its own", function.Name.Name)
	}
}
