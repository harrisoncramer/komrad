package komradelint

import (
	"go/ast"
	"go/types"

	"komradelint/config"

	"golang.org/x/tools/go/analysis"
)

const bareHTTPDoc = "Checks that an HTTP client or transport is built by lib/xhttp, " +
	"so every call komrade makes has its handshake and header waits bounded."

// newBareHTTP builds the rule that net/http's own clients and transports are not used directly.
func newBareHTTP(settings config.BareHTTPConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "bare_http",
		Doc:  bareHTTPDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				reportBareHTTP(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportBareHTTP reports net/http's DefaultClient and DefaultTransport, and a literal of its
// Client or Transport.
func reportBareHTTP(pass *analysis.Pass, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		switch expr := node.(type) {
		case *ast.SelectorExpr:
			if isNetHTTP(pass, expr) && (expr.Sel.Name == "DefaultClient" || expr.Sel.Name == "DefaultTransport") {
				pass.Reportf(expr.Pos(), "http.%s has no timeouts; build the client with xhttp.NewClient", expr.Sel.Name)
			}
		case *ast.CompositeLit:
			selector, ok := expr.Type.(*ast.SelectorExpr)
			if ok && isNetHTTP(pass, selector) && (selector.Sel.Name == "Client" || selector.Sel.Name == "Transport") {
				pass.Reportf(expr.Pos(), "a bare http.%s has no timeouts; build it with xhttp.NewClient or xhttp.NewTransport", selector.Sel.Name)
			}
		}

		return true
	})
}

// isNetHTTP says whether a selector's package is net/http.
func isNetHTTP(pass *analysis.Pass, selector *ast.SelectorExpr) bool {
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}

	name, ok := pass.TypesInfo.Uses[ident].(*types.PkgName)

	return ok && name.Imported().Path() == "net/http"
}
