package weavelint

import (
	"go/ast"
	"strings"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const constructorShapeDoc = "Checks that a constructor taking a params struct is New<T>(New<T>Params), " +
	"declared as the params, then the type, then the constructor, adjacent and in that order."

const (
	constructorPrefix = "New"
	paramsSuffix      = "Params"
)

// newConstructorShape builds the rule that a constructor's params struct is named for it and
// sits directly above its type.
func newConstructorShape(settings config.ConstructorShapeConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "constructor_shape",
		Doc:  constructorShapeDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				checkConstructors(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// checkConstructors reports every constructor in a file whose params are misnamed or out of place.
func checkConstructors(pass *analysis.Pass, file *ast.File) {
	for index, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, constructorPrefix) {
			continue
		}

		params := paramsArgument(function)
		if params == "" {
			continue
		}

		typeName := strings.TrimPrefix(function.Name.Name, constructorPrefix)
		want := function.Name.Name + paramsSuffix
		if params != want {
			pass.Reportf(function.Pos(), "%s takes %s, and a constructor's params struct is named %s", function.Name.Name, params, want)

			continue
		}

		if !isOrdered(file.Decls, index, typeName) {
			pass.Reportf(function.Pos(), "%s is declared right after %s, which is declared right after %s", function.Name.Name, typeName, want)
		}
	}
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

// isOrdered says whether the declarations before a constructor are its params and then its type.
func isOrdered(decls []ast.Decl, index int, typeName string) bool {
	params := "New" + typeName + paramsSuffix
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
