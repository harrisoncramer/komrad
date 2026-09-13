package weavelint

import (
	"go/ast"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const handlerShapeDoc = "Checks that a connect handler does nothing but hand its strict half to the adapter, " +
	"which is what keeps a failure's code decided by its type rather than by whoever wrote the handler."

// answerCall is the one thing a handler's body may be.
const answerCall = "Answer"

// newHandlerShape builds the rule that a connect handler is one line.
func newHandlerShape(settings config.HandlerShapeConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "handler_shape",
		Doc:  handlerShapeDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || isExcluded(pass, file, excluded) {
					continue
				}

				for _, decl := range file.Decls {
					function, ok := decl.(*ast.FuncDecl)
					if !ok || !answersConnect(function) {
						continue
					}

					reportShape(pass, function)
				}
			}

			return nil, nil
		},
	}, nil
}

// reportShape reports a handler whose body is anything but the one call.
func reportShape(pass *analysis.Pass, function *ast.FuncDecl) {
	if function.Body != nil && len(function.Body.List) == 1 {
		if statement, ok := function.Body.List[0].(*ast.ReturnStmt); ok && isAnswer(statement) {
			return
		}
	}

	pass.Reportf(function.Pos(),
		"a connect handler must be one line handing its strict half to the adapter, so the failure it answers with carries a kind")
}

// answersConnect says whether a function has the signature the generated interface fixes.
func answersConnect(function *ast.FuncDecl) bool {
	results := function.Type.Results
	if results == nil || len(results.List) != 2 {
		return false
	}

	return namesConnectResponse(results.List[0].Type) && isErrorType(results.List[1].Type)
}

// namesConnectResponse says whether a type is a pointer to a connect.Response of something.
func namesConnectResponse(expr ast.Expr) bool {
	pointer, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}

	index, ok := pointer.X.(*ast.IndexExpr)
	if !ok {
		return false
	}

	selector, ok := index.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	pkg, ok := selector.X.(*ast.Ident)

	return ok && pkg.Name == "connect" && selector.Sel.Name == "Response"
}

// isErrorType says whether a type is the error interface as it is spelled.
func isErrorType(expr ast.Expr) bool {
	name, ok := expr.(*ast.Ident)

	return ok && name.Name == "error"
}

// isAnswer says whether a return hands its values to the adapter.
func isAnswer(statement *ast.ReturnStmt) bool {
	if len(statement.Results) != 1 {
		return false
	}

	call, ok := statement.Results[0].(*ast.CallExpr)
	if !ok {
		return false
	}

	selector, ok := call.Fun.(*ast.SelectorExpr)

	return ok && selector.Sel.Name == answerCall
}
