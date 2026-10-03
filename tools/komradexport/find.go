package main

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"
)

const jsonschemaImport = "github.com/invopop/jsonschema"

var keywords = []string{
	"break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for", "func",
	"go", "goto", "if", "import", "interface", "map", "package", "range", "return", "select", "struct",
	"switch", "type", "var",
}

type candidate struct {
	obj      types.Object
	pkg      string
	newName  string
	receiver *types.Named
	external bool
	defined  position
	uses     map[position]bool
	idents   []usage
}

type usage struct {
	pkg   *packages.Package
	ident *ast.Ident
}

func (c *candidate) qualifiedName() string {
	if c.receiver != nil {
		return c.receiver.Obj().Name() + "." + c.obj.Name()
	}

	return c.obj.Name()
}

// find is every exported identifier nothing outside its package uses, renamed to the unexported
// name nothing in reach already holds.
func find(loaded *program, keep, generators map[string]bool, methods bool) []*candidate {
	interfaceMethods := interfaceMethodNames(loaded)
	candidates := collect(loaded, interfaceMethods, methods)
	markUses(loaded, candidates)

	var found []*candidate
	for _, c := range candidates {
		if c.external || keep[c.obj.Name()] || generators[c.obj.Name()] || len(c.uses) <= 1 {
			continue
		}

		if usedInGenerated(loaded, c) || isReserved(c.newName) || conflicts(loaded, c) {
			continue
		}

		found = append(found, c)
	}

	slices.SortFunc(found, func(a, b *candidate) int {
		return cmp.Or(cmp.Compare(a.pkg, b.pkg), cmp.Compare(a.qualifiedName(), b.qualifiedName()))
	})

	return found
}

// interfaceMethodNames is the name of every method any interface in reach declares, which a
// type may implement without naming the interface.
func interfaceMethodNames(loaded *program) map[string]bool {
	names := map[string]bool{}
	addInterface := func(t types.Type) {
		if t == nil {
			return
		}

		iface, ok := t.Underlying().(*types.Interface)
		if !ok {
			return
		}

		for method := range iface.Methods() {
			names[method.Name()] = true
		}
	}

	seen := map[*types.Package]bool{}
	var walk func(pkg *types.Package)
	walk = func(pkg *types.Package) {
		if seen[pkg] {
			return
		}

		seen[pkg] = true
		for _, name := range pkg.Scope().Names() {
			if typeName, ok := pkg.Scope().Lookup(name).(*types.TypeName); ok {
				addInterface(typeName.Type())
			}
		}

		for _, imported := range pkg.Imports() {
			walk(imported)
		}
	}

	for _, pkg := range loaded.packages {
		walk(pkg.Types)
		for _, value := range pkg.TypesInfo.Types {
			addInterface(value.Type)
		}

		for _, obj := range pkg.TypesInfo.Defs {
			if obj != nil {
				addInterface(obj.Type())
			}
		}
	}

	return names
}

func collect(loaded *program, interfaceMethods map[string]bool, methods bool) map[position]*candidate {
	candidates := map[position]*candidate{}
	for _, pkg := range loaded.packages {
		if isExternalTest(pkg) || strings.HasSuffix(pkg.PkgPath, ".test") || isGeneratedPackage(loaded.module, pkg.PkgPath) {
			continue
		}

		for ident, obj := range pkg.TypesInfo.Defs {
			if obj == nil || !obj.Exported() {
				continue
			}

			file := loaded.fset.Position(ident.Pos()).Filename
			if loaded.generated[file] || strings.HasSuffix(file, "_test.go") {
				continue
			}

			c, ok := newCandidate(pkg, obj, interfaceMethods, methods)
			if !ok {
				continue
			}

			c.defined = loaded.position(ident.Pos())
			c.uses = map[position]bool{c.defined: true}
			if _, seen := candidates[c.defined]; !seen {
				candidates[c.defined] = c
			}
		}
	}

	return candidates
}

func isGeneratedPackage(module, path string) bool {
	return strings.Contains(path, "/proto/gen/") || strings.HasPrefix(path, module+"/db/models") ||
		strings.HasPrefix(path, module+"/db/testmodels")
}

// newCandidate is the candidate an exported definition makes, or false when it has to stay
// exported whoever uses it.
func newCandidate(pkg *packages.Package, obj types.Object, interfaceMethods map[string]bool, methods bool) (*candidate, bool) {
	c := &candidate{
		obj:     obj,
		pkg:     pkg.PkgPath,
		newName: unexported(obj.Name()),
	}
	scope := pkg.Types.Scope()
	name := obj.Name()

	switch typed := obj.(type) {
	case *types.Func:
		signature, ok := typed.Type().(*types.Signature)
		if !ok {
			return nil, false
		}

		if signature.Recv() == nil {
			if typed.Parent() != scope || strings.HasPrefix(name, "New") || (pkg.Name == "main" && name == "main") {
				return nil, false
			}

			break
		}

		receiver := receiverType(signature.Recv().Type())
		if !methods || receiver == nil || interfaceMethods[name] {
			return nil, false
		}

		if _, ok := receiver.Underlying().(*types.Interface); ok {
			return nil, false
		}

		c.receiver = receiver
	case *types.TypeName:
		if _, configured := pkg.Imports[jsonschemaImport]; configured || typed.Parent() != scope {
			return nil, false
		}

		if scope.Lookup("New"+name) != nil {
			return nil, false
		}
	case *types.Const:
		if typed.Parent() != scope {
			return nil, false
		}
	case *types.Var:
		if typed.IsField() || typed.Parent() != scope {
			return nil, false
		}
	default:
		return nil, false
	}

	if strings.HasSuffix(name, "Params") || c.newName == name {
		return nil, false
	}

	return c, true
}

func receiverType(t types.Type) *types.Named {
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}

	named, ok := t.(*types.Named)
	if !ok {
		return nil
	}

	return named
}

// markUses records every use of each candidate and whether any sits outside its package.
func markUses(loaded *program, candidates map[position]*candidate) {
	for _, pkg := range loaded.packages {
		for ident, obj := range pkg.TypesInfo.Uses {
			if obj == nil || obj.Pkg() == nil {
				continue
			}

			if function, ok := obj.(*types.Func); ok {
				obj = function.Origin()
			}

			c, ok := candidates[loaded.position(obj.Pos())]
			if !ok || c.obj.Name() != obj.Name() {
				continue
			}

			if basePath(pkg) != c.pkg || isExternalTest(pkg) {
				c.external = true
			}

			c.uses[loaded.position(ident.Pos())] = true
			c.idents = append(c.idents, usage{pkg: pkg, ident: ident})
		}

		for ident, obj := range pkg.TypesInfo.Defs {
			if obj == nil {
				continue
			}

			defined := loaded.position(obj.Pos())
			if c, ok := candidates[defined]; ok && loaded.position(ident.Pos()) == defined && basePath(pkg) != c.pkg {
				c.external = true
			}
		}
	}
}

func usedInGenerated(loaded *program, c *candidate) bool {
	for use := range c.uses {
		if loaded.generated[use.file] {
			return true
		}
	}

	return false
}

func isReserved(name string) bool {
	return types.Universe.Lookup(name) != nil || slices.Contains(keywords, name)
}

// conflicts says whether the new name is already taken somewhere a use of the old one sits.
func conflicts(loaded *program, c *candidate) bool {
	if c.receiver != nil {
		for _, t := range []types.Type{c.receiver, types.NewPointer(c.receiver)} {
			if obj, _, _ := types.LookupFieldOrMethod(t, true, c.obj.Pkg(), c.newName); obj != nil {
				return true
			}
		}

		return false
	}

	if c.obj.Pkg().Scope().Lookup(c.newName) != nil {
		return true
	}

	for _, use := range c.idents {
		scope := innermostScope(use.pkg, use.ident.Pos())
		if scope == nil {
			return true
		}

		if _, obj := scope.LookupParent(c.newName, use.ident.Pos()); obj != nil {
			return true
		}
	}

	return importsName(loaded, c.pkg, c.newName)
}

func innermostScope(pkg *packages.Package, pos token.Pos) *types.Scope {
	for _, file := range pkg.Syntax {
		if pos < file.Pos() || pos > file.End() {
			continue
		}

		fileScope := pkg.TypesInfo.Scopes[file]
		if fileScope == nil {
			return pkg.Types.Scope()
		}

		return fileScope.Innermost(pos)
	}

	return nil
}

// importsName says whether any file of a package imports something under that name.
func importsName(loaded *program, pkgPath, name string) bool {
	for _, pkg := range loaded.packages {
		if basePath(pkg) != pkgPath {
			continue
		}

		for _, file := range pkg.Syntax {
			for _, spec := range file.Imports {
				imported := strings.Trim(spec.Path.Value, `"`)
				imported = imported[strings.LastIndex(imported, "/")+1:]
				if spec.Name != nil {
					imported = spec.Name.Name
				}

				if imported == name {
					return true
				}
			}
		}
	}

	return false
}

// unexported lowercases a name's leading capitals, keeping the last of a run that starts the
// next word.
func unexported(name string) string {
	runes := []rune(name)
	upper := 0
	for upper < len(runes) && unicode.IsUpper(runes[upper]) {
		upper++
	}

	if upper == len(runes) {
		return strings.ToLower(name)
	}

	if upper > 1 && unicode.IsLower(runes[upper]) {
		upper--
	}

	for index := range upper {
		runes[index] = unicode.ToLower(runes[index])
	}

	return string(runes)
}
