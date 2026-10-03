package komradlint

import (
	"go/ast"
	"go/types"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const errorTextDoc = "Checks that a test matches an error's text through assert.ErrorContains or " +
	"assert.EqualError instead of calling Error on it, so a nil error fails rather than panics."

// errorTextChecks maps each testify check to the argument it reads the error's text from and
// the check that takes the error itself.
var errorTextChecks = map[string]struct {
	arg  int
	want string
}{
	"Contains":  {arg: 1, want: "ErrorContains"},
	"Containsf": {arg: 1, want: "ErrorContainsf"},
	"Equal":     {arg: 2, want: "EqualError"},
	"Equalf":    {arg: 2, want: "EqualErrorf"},
}

// testifyPackages are the packages whose checks the rule reads.
var testifyPackages = map[string]bool{
	"github.com/stretchr/testify/assert":  true,
	"github.com/stretchr/testify/require": true,
}

// newErrorText builds the rule that a test hands testify the error, not its text.
func newErrorText(settings config.ErrorTextConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "error_text",
		Doc:  errorTextDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isUnder(pass, file, excluded) {
					continue
				}

				reportErrorTexts(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportErrorTexts reports every testify check in a file that reads an error's text.
func reportErrorTexts(pass *analysis.Pass, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		name := testifyCheck(pass, call)

		check, ok := errorTextChecks[name]
		if !ok || len(call.Args) <= check.arg || !isErrorText(pass, call.Args[check.arg]) {
			return true
		}

		pass.Reportf(call.Pos(), "a test hands %s the error itself, not the text of %s", check.want, name)

		return true
	})
}

// testifyCheck returns the name of the assert or require function a call makes, or nothing.
func testifyCheck(pass *analysis.Pass, call *ast.CallExpr) string {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}

	function, ok := pass.TypesInfo.Uses[selector.Sel].(*types.Func)
	if !ok || function.Pkg() == nil || !testifyPackages[function.Pkg().Path()] || function.Signature().Recv() != nil {
		return ""
	}

	return function.Name()
}

// isErrorText says whether an expression calls Error on an error.
func isErrorText(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) > 0 {
		return false
	}

	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Error" {
		return false
	}

	return isError(pass.TypesInfo.TypeOf(selector.X))
}
