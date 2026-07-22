// Package forbidgetenv flags direct process-environment access so configuration
// flows through the config package (backed by ultra) instead of bare os calls.
package forbidgetenv

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// forbidden is the set of os functions that read or mutate the environment.
var forbidden = map[string]struct{}{
	"Getenv":    {},
	"LookupEnv": {},
	"Environ":   {},
	"Setenv":    {},
	"Unsetenv":  {},
	"Clearenv":  {},
	"ExpandEnv": {},
}

// NewAnalyzer returns the analyzer that flags forbidden os environment calls.
func NewAnalyzer() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:             "forbidgetenv",
		Doc:              "forbid direct os environment access; read configuration through the config package",
		RunDespiteErrors: true,
		Run:              run,
	}
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}

		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			if _, bad := forbidden[selector.Sel.Name]; bad && isStdOs(pass, selector.X) {
				pass.Report(analysis.Diagnostic{
					Pos:     selector.Pos(),
					End:     selector.End(),
					Message: fmt.Sprintf("do not call os.%s directly; read configuration through the config package", selector.Sel.Name),
				})
			}

			return true
		})
	}

	return nil, nil
}

// isStdOs reports whether expr references the standard library os package.
func isStdOs(pass *analysis.Pass, expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}

	pkg, ok := pass.TypesInfo.Uses[ident].(*types.PkgName)
	return ok && pkg.Imported().Path() == "os"
}
