package komradlint

import (
	"go/ast"
	"go/token"
	"go/types"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const ctxSleepDoc = "Checks that a select which only waits out a delay unless its context ends " +
	"is written as clock.Sleep followed by a check of ctx.Err, so a pause reads as one."

// newCtxSleep builds the rule that a hand-rolled cancellable sleep is a clock.Sleep call.
func newCtxSleep(settings config.CtxSleepConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "ctx_sleep",
		Doc:  ctxSleepDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isUnder(pass, file, excluded) {
					continue
				}

				reportCtxSleeps(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportCtxSleeps reports every select in a file that only sleeps until its context ends.
func reportCtxSleeps(pass *analysis.Pass, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		stmt, ok := node.(*ast.SelectStmt)
		if !ok || !isCtxSleep(pass, stmt) {
			return true
		}

		pass.Reportf(stmt.Pos(), "select only sleeps until the context ends: call clock.Sleep and then check ctx.Err")

		return true
	})
}

// isCtxSleep says whether a select has one arm for its context ending and one for a delay
// passing, each doing nothing or leaving at once.
func isCtxSleep(pass *analysis.Pass, stmt *ast.SelectStmt) bool {
	if len(stmt.Body.List) != 2 {
		return false
	}

	var done, delay bool
	for _, entry := range stmt.Body.List {
		clause, ok := entry.(*ast.CommClause)
		if !ok || !isBareExit(clause.Body) {
			return false
		}

		received, ok := receivedFrom(clause.Comm)
		if !ok {
			return false
		}

		switch {
		case isContextDone(pass, received):
			done = true
		case isDelay(pass, received):
			delay = true
		}
	}

	return done && delay
}

// isBareExit says whether an arm's body is empty or a lone return, break, continue or goto.
func isBareExit(body []ast.Stmt) bool {
	if len(body) == 0 {
		return true
	}

	if len(body) != 1 {
		return false
	}

	switch body[0].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	}

	return false
}

// receivedFrom is the channel an arm receives from without keeping what it received.
func receivedFrom(comm ast.Stmt) (ast.Expr, bool) {
	stmt, ok := comm.(*ast.ExprStmt)
	if !ok {
		return nil, false
	}

	receive, ok := ast.Unparen(stmt.X).(*ast.UnaryExpr)
	if !ok || receive.Op != token.ARROW {
		return nil, false
	}

	return receive.X, true
}

// isContextDone says whether an expression is a call of Done on a context.Context.
func isContextDone(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}

	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Done" {
		return false
	}

	return isNamed(pass.TypesInfo.TypeOf(selector.X), "context", "Context")
}

// isDelay says whether an expression is a time.After call or the channel of a *time.Timer.
func isDelay(pass *analysis.Pass, expr ast.Expr) bool {
	switch expr := ast.Unparen(expr).(type) {
	case *ast.CallExpr:
		selector, ok := expr.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}

		called, ok := pass.TypesInfo.Uses[selector.Sel].(*types.Func)

		return ok && called.Pkg() != nil && called.Pkg().Path() == "time" && called.Name() == "After"
	case *ast.SelectorExpr:
		if expr.Sel.Name != "C" {
			return false
		}

		pointer, ok := pass.TypesInfo.TypeOf(expr.X).(*types.Pointer)

		return ok && isNamed(pointer.Elem(), "time", "Timer")
	}

	return false
}

// isNamed says whether a type is the named type a package declares under that name.
func isNamed(typ types.Type, pkg, name string) bool {
	named, ok := typ.(*types.Named)
	if !ok {
		return false
	}

	object := named.Obj()

	return object.Pkg() != nil && object.Pkg().Path() == pkg && object.Name() == name
}
