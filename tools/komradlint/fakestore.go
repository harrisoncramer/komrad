package komradlint

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const fakeStoreDoc = "Checks that no test declares a method standing in for a store's own, " +
	"which is what a fake store looks like, so a service is tested against the real store."

// storePackage is where every store package lives.
const storePackage = "komrad/internal/store"

// newFakeStore builds the rule that a test runs against the real store rather than a fake of it.
func newFakeStore(settings config.FakeStoreConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "fake_store",
		Doc:  fakeStoreDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if !isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				reportFakeStores(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportFakeStores reports every method in a test file whose signature is a store method's and names a store type.
func reportFakeStores(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv == nil || !function.Name.IsExported() {
			continue
		}

		method, ok := pass.TypesInfo.Defs[function.Name].(*types.Func)
		if !ok {
			continue
		}

		receiver := namedOf(pass.TypesInfo.TypeOf(function.Recv.List[0].Type))
		if receiver == nil {
			continue
		}

		fields := function.Type.Params.List
		if function.Type.Results != nil {
			fields = slices.Concat(fields, function.Type.Results.List)
		}

		for _, field := range fields {
			named := namedOf(pass.TypesInfo.TypeOf(field.Type))
			if named == nil || !isStoreType(named) {
				continue
			}

			original := storeMethod(named.Obj().Pkg(), method)
			if original == "" {
				continue
			}

			pass.Reportf(function.Name.Pos(), "%s.%s stands in for %s; test against the real store through storetest", receiver.Obj().Name(), function.Name.Name, original)

			break
		}
	}
}

// isStoreType says whether a type is declared in a store package.
func isStoreType(named *types.Named) bool {
	pkg := named.Obj().Pkg()
	if pkg == nil {
		return false
	}

	return pkg.Path() == storePackage || strings.HasPrefix(pkg.Path(), storePackage+"/")
}

// storeMethod names the method of a store package's type that a test method matches in name and signature, or nothing.
func storeMethod(pkg *types.Package, method *types.Func) string {
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		typeName, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || typeName.IsAlias() {
			continue
		}

		object, _, _ := types.LookupFieldOrMethod(types.NewPointer(typeName.Type()), true, pkg, method.Name())
		candidate, ok := object.(*types.Func)
		if !ok || !types.Identical(candidate.Type(), method.Type()) {
			continue
		}

		return pkg.Name() + "." + name + "." + method.Name()
	}

	return ""
}
