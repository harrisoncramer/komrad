package weavelint

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const sharedFakeDoc = "Checks that no test declares its own fake of a boundary a helper package " +
	"already fakes, so tmux, the harness and git are each faked once."

// newSharedFake builds the rule that a boundary with a shared fake is faked by it alone.
func newSharedFake(settings config.SharedFakeConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "shared_fake",
		Doc:  sharedFakeDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			boundaries := resolveBoundaries(pass.Pkg, settings.Fakes)
			if len(boundaries) == 0 {
				return nil, nil
			}

			for _, file := range pass.Files {
				if !isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				reportSharedFakes(pass, file, boundaries)
			}

			return nil, nil
		},
	}, nil
}

// boundary is a configured interface this package can see, and the shared fake that stands in for it.
type boundary struct {
	name  string
	iface *types.Interface
	fake  string
}

// resolveBoundaries finds each configured interface among a package and everything it imports.
func resolveBoundaries(pkg *types.Package, fakes []config.SharedFake) []boundary {
	seen := map[string]*types.Package{}
	walkImports(pkg, seen)

	var boundaries []boundary
	for _, fake := range fakes {
		dot := strings.LastIndex(fake.Interface, ".")
		if dot < 0 {
			continue
		}

		owner, ok := seen[fake.Interface[:dot]]
		if !ok {
			continue
		}

		name, ok := owner.Scope().Lookup(fake.Interface[dot+1:]).(*types.TypeName)
		if !ok {
			continue
		}

		iface, ok := name.Type().Underlying().(*types.Interface)
		if !ok || iface.NumMethods() == 0 {
			continue
		}

		boundaries = append(boundaries, boundary{name: owner.Name() + "." + name.Name(), iface: iface, fake: fake.Fake})
	}

	return boundaries
}

// walkImports records a package and everything it imports, by import path.
func walkImports(pkg *types.Package, seen map[string]*types.Package) {
	if _, ok := seen[pkg.Path()]; ok {
		return
	}

	seen[pkg.Path()] = pkg
	for _, imported := range pkg.Imports() {
		walkImports(imported, seen)
	}
}

// reportSharedFakes reports every type a test file declares that satisfies a boundary with a shared fake.
func reportSharedFakes(pass *analysis.Pass, file *ast.File, boundaries []boundary) {
	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}

		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			name, ok := pass.TypesInfo.Defs[typeSpec.Name].(*types.TypeName)
			if !ok || types.IsInterface(name.Type()) {
				continue
			}

			for _, each := range boundaries {
				if !types.Implements(name.Type(), each.iface) && !types.Implements(types.NewPointer(name.Type()), each.iface) {
					continue
				}

				pass.Reportf(typeSpec.Name.Pos(), "%s fakes %s, which %s already fakes; use that instead", name.Name(), each.name, each.fake)

				break
			}
		}
	}
}
