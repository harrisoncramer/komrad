package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/packages"
)

type position struct {
	file   string
	offset int
}

type program struct {
	fset      *token.FileSet
	packages  []*packages.Package
	module    string
	root      string
	generated map[string]bool
}

func load(patterns []string, tags string) (*program, error) {
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedImports | packages.NeedModule,
		Tests:      true,
		BuildFlags: []string{"-tags=" + tags},
	}

	loaded, err := packages.Load(config, patterns...)
	if err != nil {
		return nil, fmt.Errorf("failed to load packages: %w", err)
	}

	if packages.PrintErrors(loaded) > 0 {
		return nil, fmt.Errorf("packages do not build")
	}

	if len(loaded) == 0 {
		return &program{fset: token.NewFileSet(), generated: map[string]bool{}}, nil
	}

	loadedProgram := &program{
		fset:      loaded[0].Fset,
		packages:  loaded,
		generated: map[string]bool{},
	}

	for _, pkg := range loaded {
		if pkg.Module != nil && pkg.Module.Main {
			loadedProgram.module = pkg.Module.Path
			loadedProgram.root = pkg.Module.Dir
		}

		for _, file := range pkg.Syntax {
			if ast.IsGenerated(file) {
				loadedProgram.generated[loadedProgram.fset.Position(file.Pos()).Filename] = true
			}
		}
	}

	return loadedProgram, nil
}

func (p *program) position(pos token.Pos) position {
	resolved := p.fset.Position(pos)

	return position{file: resolved.Filename, offset: resolved.Offset}
}

// basePath is the import path of the package a test variant belongs to.
func basePath(pkg *packages.Package) string {
	return strings.TrimSuffix(pkg.PkgPath, "_test")
}

// isExternalTest says whether a package is the _test package beside the one it tests.
func isExternalTest(pkg *packages.Package) bool {
	return strings.HasSuffix(pkg.PkgPath, "_test")
}
