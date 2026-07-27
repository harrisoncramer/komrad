// Package forbidgetenv flags direct process-environment access so configuration
// flows through the config package instead of bare os calls.
package forbidgetenv

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"agentslinter/config"

	"golang.org/x/tools/go/analysis"
)

// forbidden is the set of os functions that read a configuration value out of
// the environment. Process-env management (Environ, Setenv, Unsetenv) is left
// alone because spawning child processes legitimately needs it.
var forbidden = map[string]struct{}{
	"Getenv":    {},
	"LookupEnv": {},
}

// NewAnalyzer returns the analyzer that flags forbidden os environment calls.
func NewAnalyzer(_ config.ForbidGetenvConfig) *analysis.Analyzer {
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
