package komradelint

import (
	"go/ast"
	"go/types"

	"komradelint/config"

	"golang.org/x/tools/go/analysis"
)

const blackBoxDoc = "Checks that a test inside a package never drives the types held behind " +
	"its external test package, so a screen test asserts on what the panel draws and sends."

// newBlackBox builds the rule that the configured types are driven only from the external test package.
func newBlackBox(settings config.BlackBoxConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	held := make(map[string]struct{}, len(settings.Types))
	for _, name := range settings.Types {
		held[name] = struct{}{}
	}

	return &analysis.Analyzer{
		Name: "black_box",
		Doc:  blackBoxDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if !isTest(pass, file) || file.Name.Name != pass.Pkg.Name() || isUnder(pass, file, excluded) {
					continue
				}

				reportBlackBox(pass, file, held)
			}

			return nil, nil
		},
	}, nil
}

// reportBlackBox reports the first place a test file names a value of a held type.
func reportBlackBox(pass *analysis.Pass, file *ast.File, held map[string]struct{}) {
	reported := false
	ast.Inspect(file, func(node ast.Node) bool {
		if reported {
			return false
		}

		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}

		named := namedOf(pass.TypesInfo.TypeOf(ident))
		if named == nil {
			return true
		}

		name := typePath(named)
		if _, ok := held[name]; !ok {
			return true
		}

		pass.Reportf(ident.Pos(), "a test inside %s drives %s; write it in package %s_test and drive it through the harness", pass.Pkg.Name(), named.Obj().Name(), pass.Pkg.Name())
		reported = true

		return false
	})
}

// namedOf is the named type behind any pointers, slices and maps, or nil for any other type.
func namedOf(typ types.Type) *types.Named {
	for typ != nil {
		switch value := typ.(type) {
		case *types.Named:
			return value.Origin()
		case *types.Pointer:
			typ = value.Elem()
		case *types.Slice:
			typ = value.Elem()
		case *types.Map:
			typ = value.Elem()
		default:
			return nil
		}
	}

	return nil
}

// typePath is a named type spelled as its import path, a dot and its name.
func typePath(named *types.Named) string {
	if named.Obj().Pkg() == nil {
		return named.Obj().Name()
	}

	return named.Obj().Pkg().Path() + "." + named.Obj().Name()
}
