package komradlint

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const wrapPhraseDoc = "Checks that a fmt.Errorf wrapping another error with a trailing ': %w' " +
	"opens with 'failed to ' or with the wrapped error itself."

// newWrapPhrase builds the rule that a wrapping error message opens the one way.
func newWrapPhrase(settings config.WrapPhraseConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "wrap_phrase",
		Doc:  wrapPhraseDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				reportWrapPhrases(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportWrapPhrases reports every fmt.Errorf format that wraps with ': %w' and opens otherwise.
func reportWrapPhrases(pass *analysis.Pass, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isFmtErrorf(call) || len(call.Args) == 0 {
			return true
		}

		format, ok := stringLiteral(call.Args[0])
		if !ok || !strings.HasSuffix(format, ": %w") {
			return true
		}

		if strings.HasPrefix(format, "failed to ") || strings.HasPrefix(format, "%w") {
			return true
		}

		pass.Reportf(call.Args[0].Pos(),
			"a message wrapping another error opens with \"failed to \" or with %%w: rewrite %q", format)

		return true
	})
}

// isFmtErrorf says whether a call is fmt.Errorf.
func isFmtErrorf(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	pkg, ok := selector.X.(*ast.Ident)

	return ok && pkg.Name == "fmt" && selector.Sel.Name == "Errorf"
}

// stringLiteral returns the value of a string literal, if the expression is one.
func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}

	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}

	return value, true
}
