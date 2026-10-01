package komradlint

import (
	"go/ast"
	"strings"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const paramsShapeDoc = "Checks that a params struct named for a function is declared directly above it, " +
	"and that a constructor taking one is New<T>(New<T>Params), declared as the params, then the type, " +
	"then the constructor, adjacent and in that order."

const (
	constructorPrefix = "New"
	paramsSuffix      = "Params"
)

// newParamsShape builds the rule that a params struct is named for its function and sits
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

			var sources, checked []*ast.File
			for _, file := range pass.Files {
				if isTest(pass, file) {
					continue
				}

				sources = append(sources, file)
				if isUnder(pass, file, excluded) {
					continue
				}

				checked = append(checked, file)
				checkConstructors(pass, file)
			}

			checkParamsPlacement(pass, sources, checked)

			return nil, nil
		},
	}, nil
}

// checkConstructors reports every constructor in a file whose params are misnamed or out of place.
func checkConstructors(pass *analysis.Pass, file *ast.File) {
	for index, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || !isConstructor(function) {
			continue
		}

		params := paramsArgument(function)
		if params == "" {
			continue
		}

		typeName := strings.TrimPrefix(function.Name.Name, constructorPrefix)
		want := function.Name.Name + paramsSuffix
		if params != want {
			pass.Reportf(function.Pos(), "%s takes %s: name it %s", function.Name.Name, params, want)

			continue
		}

		if isOrdered(file.Decls, index, typeName, want) {
			continue
		}

		if declaresType(file.Decls, typeName) {
			pass.Reportf(function.Pos(), "%s is out of order: declare %s, then %s, then %s, one after another", function.Name.Name, want, typeName, function.Name.Name)
		} else {
			pass.Reportf(function.Pos(), "%s is out of order: declare %s directly above %s", function.Name.Name, want, function.Name.Name)
		}
	}
}

// checkParamsPlacement reports every function taking its own params struct when that struct
// sits directly above none of the functions named for it.
func checkParamsPlacement(pass *analysis.Pass, sources, checked []*ast.File) {
	takers := map[string][]*ast.FuncDecl{}
	for _, file := range checked {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || isConstructor(function) {
				continue
			}

			params := ownParams(function)
			if params == "" {
				continue
			}

			takers[params] = append(takers[params], function)
		}
	}

	for params, functions := range takers {
		if isPlaced(sources, params) {
			continue
		}

		for _, function := range functions {
			pass.Reportf(function.Pos(), "%s is out of place: declare %s directly above %s", function.Name.Name, params, function.Name.Name)
		}
	}
}

// isConstructor says whether a declaration is a free function named New something.
func isConstructor(function *ast.FuncDecl) bool {
	return function.Recv == nil && strings.HasPrefix(function.Name.Name, constructorPrefix)
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

// ownParams is the name of the params struct a function takes that is named for it, or empty
// when it takes none.
func ownParams(function *ast.FuncDecl) string {
	want := function.Name.Name + paramsSuffix
	for _, field := range function.Type.Params.List {
		name := localTypeName(field.Type)
		if strings.EqualFold(name, want) {
			return name
		}
	}

	return ""
}

// isPlaced says whether a params struct is declared directly above a function named for it,
// with only that function's receiver type allowed between them.
func isPlaced(files []*ast.File, params string) bool {
	for _, file := range files {
		for index, decl := range file.Decls {
			if !isTypeDecl(decl, params) || index+1 >= len(file.Decls) {
				continue
			}

			next, ok := file.Decls[index+1].(*ast.FuncDecl)
			if ok && ownParams(next) == params {
				return true
			}

			if index+2 >= len(file.Decls) {
				continue
			}

			method, ok := file.Decls[index+2].(*ast.FuncDecl)
			if ok && ownParams(method) == params && isTypeDecl(file.Decls[index+1], receiverName(method)) {
				return true
			}
		}
	}

	return false
}

// receiverName is the name of the type a method is declared on, or empty for a free function.
func receiverName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return ""
	}

	return localTypeName(function.Recv.List[0].Type)
}

// localTypeName is the name of the package's own type an expression refers to, through a pointer
// or type arguments, or empty when it names none.
func localTypeName(expr ast.Expr) string {
	if pointer, ok := expr.(*ast.StarExpr); ok {
		expr = pointer.X
	}

	switch generic := expr.(type) {
	case *ast.IndexExpr:
		expr = generic.X
	case *ast.IndexListExpr:
		expr = generic.X
	}

	name, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}

	return name.Name
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
