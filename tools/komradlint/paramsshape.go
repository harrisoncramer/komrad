package komradlint

import (
	"go/ast"
	"strings"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const paramsShapeDoc = "Checks that a function taking a params struct named for it declares that struct directly " +
	"above it, with a constructor's params, type and constructor adjacent and in that order."

const (
	constructorPrefix = "New"
	paramsSuffix      = "Params"
)

// newParamsShape builds the rule that a function's params struct is named for it and sits
// directly above it.
func newParamsShape(settings config.ParamsShapeConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "params_shape",
		Doc:  paramsShapeDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				checkParams(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// checkParams reports every function in a file whose params struct is misnamed or out of place.
func checkParams(pass *analysis.Pass, file *ast.File) {
	for index, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		if function.Recv == nil && strings.HasPrefix(function.Name.Name, constructorPrefix) {
			checkConstructor(pass, file.Decls, index, function)

			continue
		}

		checkFunction(pass, file.Decls, index, function)
	}
}

// checkConstructor reports a constructor whose params are misnamed or out of place.
func checkConstructor(pass *analysis.Pass, decls []ast.Decl, index int, function *ast.FuncDecl) {
	params := paramsArgument(function)
	if params == "" {
		return
	}

	typeName := strings.TrimPrefix(function.Name.Name, constructorPrefix)
	want := function.Name.Name + paramsSuffix
	if params != want {
		pass.Reportf(function.Pos(), "%s takes %s: name it %s", function.Name.Name, params, want)

		return
	}

	if isOrdered(decls, index, typeName, want) {
		return
	}

	if declaresType(decls, typeName) {
		pass.Reportf(function.Pos(), "%s is out of order: declare %s, then %s, then %s, one after another", function.Name.Name, want, typeName, function.Name.Name)
	} else {
		pass.Reportf(function.Pos(), "%s is out of order: declare %s directly above %s", function.Name.Name, want, function.Name.Name)
	}
}

// checkFunction reports a function whose own params struct is declared in its file but not
// directly above it, or above its receiver's type.
func checkFunction(pass *analysis.Pass, decls []ast.Decl, index int, function *ast.FuncDecl) {
	want := function.Name.Name + paramsSuffix
	if !takesType(function, want) || !declaresType(decls, want) {
		return
	}

	if index >= 1 && isTypeDecl(decls[index-1], want) {
		return
	}

	receiver := receiverName(function)
	if receiver != "" && index >= 2 && isTypeDecl(decls[index-1], receiver) && isTypeDecl(decls[index-2], want) {
		return
	}

	pass.Reportf(function.Pos(), "%s is out of order: declare %s directly above %s", function.Name.Name, want, function.Name.Name)
}

// paramsArgument is the name of the params struct a function takes, or empty when it takes none.
func paramsArgument(function *ast.FuncDecl) string {
	for _, field := range function.Type.Params.List {
		name, ok := field.Type.(*ast.Ident)
		if ok && strings.HasSuffix(name.Name, paramsSuffix) {
			return name.Name
		}
	}

	return ""
}

// takesType says whether a function has a parameter of the named type from its own package.
func takesType(function *ast.FuncDecl, name string) bool {
	for _, field := range function.Type.Params.List {
		ident, ok := field.Type.(*ast.Ident)
		if ok && ident.Name == name {
			return true
		}
	}

	return false
}

// receiverName is the name of a method's receiver type, or empty for a plain function.
func receiverName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return ""
	}

	expr := function.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}

	switch typed := expr.(type) {
	case *ast.IndexExpr:
		expr = typed.X
	case *ast.IndexListExpr:
		expr = typed.X
	}

	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}

	return ident.Name
}

// isOrdered says whether the declarations before a constructor are its params and then its type.
func isOrdered(decls []ast.Decl, index int, typeName, params string) bool {
	if declaresType(decls, typeName) {
		return index >= 2 && isTypeDecl(decls[index-1], typeName) && isTypeDecl(decls[index-2], params)
	}

	return index >= 1 && isTypeDecl(decls[index-1], params)
}

// declaresType says whether a type of that name is declared among the declarations.
func declaresType(decls []ast.Decl, name string) bool {
	for _, decl := range decls {
		if isTypeDecl(decl, name) {
			return true
		}
	}

	return false
}

// isTypeDecl says whether a declaration is the one type of that name.
func isTypeDecl(decl ast.Decl, name string) bool {
	general, ok := decl.(*ast.GenDecl)
	if !ok || len(general.Specs) != 1 {
		return false
	}

	spec, ok := general.Specs[0].(*ast.TypeSpec)

	return ok && spec.Name.Name == name
}
