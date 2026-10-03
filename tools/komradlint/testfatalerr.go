package komradlint

import (
	"go/ast"
	"go/types"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const testFatalErrDoc = "Checks that a test hands an error to require.NoError or assert.NoError " +
	"instead of formatting it into t.Fatal or t.Error, so a failure reads the same everywhere."

// testFailures are the testing methods that fail a test.
var testFailures = map[string]bool{
	"Fatal":  true,
	"Fatalf": true,
	"Error":  true,
	"Errorf": true,
}

// newTestFatalErr builds the rule that a test fails on an error through testify.
func newTestFatalErr(settings config.FatalErrConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	covered, err := compile(settings.Packages)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "test_fatal_err",
		Doc:  testFatalErrDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip || !isCovered(pass.Pkg.Path(), covered) {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isUnder(pass, file, excluded) {
					continue
				}

				reportTestFatalErrs(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportTestFatalErrs reports every t.Fatal or t.Error in a file that is handed an error.
func reportTestFatalErrs(pass *analysis.Pass, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isTestFailure(pass, call) || !hasErrorArg(pass, call) {
			return true
		}

		pass.Reportf(call.Pos(), "a test fails on an error through require.NoError or assert.NoError, not t.Fatal or t.Error")

		return true
	})
}

// isTestFailure says whether a call is Fatal, Fatalf, Error or Errorf of the testing package.
func isTestFailure(pass *analysis.Pass, call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !testFailures[selector.Sel.Name] {
		return false
	}

	method, ok := pass.TypesInfo.Uses[selector.Sel].(*types.Func)

	return ok && method.Pkg() != nil && method.Pkg().Path() == "testing"
}

// hasErrorArg says whether any argument of a call is an error.
func hasErrorArg(pass *analysis.Pass, call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if isError(pass.TypesInfo.TypeOf(arg)) {
			return true
		}
	}

	return false
}
