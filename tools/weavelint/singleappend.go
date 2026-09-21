package weavelint

import (
	"go/ast"
	"go/token"
	"go/types"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const singleAppendDoc = "Checks that a range loop over a slice whose whole body appends one value " +
	"per element is written as xslice.Map, so a mapping reads as one."

// newSingleAppend builds the rule that a one-line mapping loop is an xslice.Map call.
func newSingleAppend(settings config.SingleAppendConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "single_append",
		Doc:  singleAppendDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isUnder(pass, file, excluded) {
					continue
				}

				reportSingleAppends(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportSingleAppends reports every range loop in a file that only maps its slice.
func reportSingleAppends(pass *analysis.Pass, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		loop, ok := node.(*ast.RangeStmt)
		if !ok || !isMappingLoop(pass, loop) {
			return true
		}

		pass.Reportf(loop.Pos(), "range loop only appends one value per element: write it as xslice.Map")

		return true
	})
}

// isMappingLoop says whether a range loop is one element in, one append out, over a slice.
func isMappingLoop(pass *analysis.Pass, loop *ast.RangeStmt) bool {
	value, ok := valueOnly(loop)
	if !ok || !isSlice(pass, loop.X) || len(loop.Body.List) != 1 {
		return false
	}

	target, appended, ok := singleAppend(loop.Body.List[0])
	if !ok || sameExpr(target, loop.X) || rootIdent(target) == value {
		return false
	}

	switch appended := appended.(type) {
	case *ast.IndexExpr, *ast.IndexListExpr:
		return false
	case *ast.Ident:
		return appended.Name != value
	}

	return true
}

// rootIdent is the identifier a selector path starts from, or empty when there is none.
func rootIdent(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.Ident:
			return e.Name
		case *ast.SelectorExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		default:
			return ""
		}
	}
}

// valueOnly is the name a range loop binds its element to, when it leaves its index blank.
func valueOnly(loop *ast.RangeStmt) (string, bool) {
	if loop.Tok != token.DEFINE {
		return "", false
	}

	value, ok := loop.Value.(*ast.Ident)
	if !ok || value.Name == "_" {
		return "", false
	}

	if loop.Key == nil {
		return value.Name, true
	}

	key, ok := loop.Key.(*ast.Ident)
	if !ok || key.Name != "_" {
		return "", false
	}

	return value.Name, true
}

// isSlice says whether an expression's type is a slice.
func isSlice(pass *analysis.Pass, expr ast.Expr) bool {
	typ := pass.TypesInfo.TypeOf(expr)
	if typ == nil {
		return false
	}

	_, ok := typ.Underlying().(*types.Slice)

	return ok
}

// singleAppend reads a statement as x = append(x, e), returning x and e.
func singleAppend(stmt ast.Stmt) (ast.Expr, ast.Expr, bool) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, nil, false
	}

	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 2 || call.Ellipsis.IsValid() {
		return nil, nil, false
	}

	name, ok := call.Fun.(*ast.Ident)
	if !ok || name.Name != "append" || !sameExpr(assign.Lhs[0], call.Args[0]) {
		return nil, nil, false
	}

	return assign.Lhs[0], call.Args[1], true
}

// sameExpr says whether two expressions spell the same identifier or selector path.
func sameExpr(a, b ast.Expr) bool {
	switch a := a.(type) {
	case *ast.Ident:
		b, ok := b.(*ast.Ident)

		return ok && a.Name == b.Name
	case *ast.SelectorExpr:
		b, ok := b.(*ast.SelectorExpr)

		return ok && a.Sel.Name == b.Sel.Name && sameExpr(a.X, b.X)
	case *ast.ParenExpr:
		return sameExpr(a.X, b)
	}

	return false
}
