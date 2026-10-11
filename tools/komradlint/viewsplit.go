package komradlint

import (
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const viewSplitDoc = "Checks that a screen's drawing sits in a view file beside the file holding its state, " +
	"and that the drawing there reads state without changing it."

const viewFileSuffix = "view.go"

// newViewSplit builds the rule that drawing lives in view files and never writes the state it reads.
func newViewSplit(settings config.ViewSplitConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	covered, err := compile(settings.Packages)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "view_split",
		Doc:  viewSplitDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip || !isCovered(pass.Pkg.Path(), covered) {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				reportViewSplit(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportViewSplit reports the writes a view file's functions make and the drawing any other file holds.
func reportViewSplit(pass *analysis.Pass, file *ast.File) {
	inView := isViewFile(pass.Fset.Position(file.Pos()).Filename)
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}

		if inView {
			reportViewWrites(pass, function)

			continue
		}

		if isDrawing(pass, function) {
			pass.Reportf(function.Name.Pos(), "%s draws, so it belongs in a file ending in %s beside the file holding its state", function.Name.Name, viewFileSuffix)
		}
	}
}

// isViewFile says whether a file is named for another beside it with view put before the extension.
func isViewFile(path string) bool {
	base := filepath.Base(path)
	stem, ok := strings.CutSuffix(base, viewFileSuffix)
	if !ok || stem == "" {
		return false
	}

	_, err := os.Stat(filepath.Join(filepath.Dir(path), stem+".go"))

	return err == nil
}

// isDrawing says whether a function is named as a view and returns the string it draws.
func isDrawing(pass *analysis.Pass, function *ast.FuncDecl) bool {
	name := function.Name.Name
	if name != "view" && !strings.HasSuffix(name, "View") {
		return false
	}

	obj, ok := pass.TypesInfo.Defs[function.Name].(*types.Func)
	if !ok {
		return false
	}

	sig, ok := obj.Type().(*types.Signature)
	if !ok {
		return false
	}

	results := sig.Results()
	if results.Len() != 1 {
		return false
	}

	basic, ok := results.At(0).Type().Underlying().(*types.Basic)

	return ok && basic.Kind() == types.String
}

// reportViewWrites reports every write a view function makes that reaches its caller through its receiver or parameters.
func reportViewWrites(pass *analysis.Pass, function *ast.FuncDecl) {
	held := map[types.Object]struct{}{}
	for _, fields := range []*ast.FieldList{function.Recv, function.Type.Params} {
		if fields == nil {
			continue
		}

		for _, field := range fields.List {
			for _, name := range field.Names {
				if obj := pass.TypesInfo.Defs[name]; obj != nil {
					held[obj] = struct{}{}
				}
			}
		}
	}

	report := func(target ast.Expr) {
		root, ok := sharedWriteRoot(pass, target)
		if !ok {
			return
		}

		if _, ok := held[pass.TypesInfo.Uses[root]]; ok {
			pass.Reportf(target.Pos(), "%s writes %s while it draws; change state where a message is handled and let the view read it", function.Name.Name, root.Name)
		}
	}

	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			if stmt.Tok == token.DEFINE {
				return true
			}

			for _, lhs := range stmt.Lhs {
				report(lhs)
			}
		case *ast.IncDecStmt:
			report(stmt.X)
		}

		return true
	})
}

// sharedWriteRoot is the variable a write starts from when the write passes through a pointer, map or slice, so the caller sees it.
func sharedWriteRoot(pass *analysis.Pass, expr ast.Expr) (*ast.Ident, bool) {
	shared := false
	for {
		var inner ast.Expr
		switch value := ast.Unparen(expr).(type) {
		case *ast.SelectorExpr:
			inner = value.X
		case *ast.IndexExpr:
			inner = value.X
		case *ast.StarExpr:
			inner = value.X
			shared = true
		case *ast.Ident:
			return value, shared
		default:
			return nil, false
		}

		switch pass.TypesInfo.TypeOf(inner).Underlying().(type) {
		case *types.Pointer, *types.Map, *types.Slice:
			shared = true
		}

		expr = inner
	}
}
