package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var word = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

var ignoreConstraint = []byte("//go:build ignore")

// keptNames is every word in the files given, any of which may name an identifier a linter
// config relies on.
func keptNames(files []string) (map[string]bool, error) {
	kept := map[string]bool{}
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}

		for _, name := range word.FindAll(contents, -1) {
			kept[string(name)] = true
		}
	}

	return kept, nil
}

// generatorUses is every name the module's go:build ignore programs select from one of its
// own packages, which no package load sees.
func generatorUses(loaded *program) (map[string]bool, error) {
	used := map[string]bool{}
	if loaded.root == "" {
		return used, nil
	}

	err := filepath.WalkDir(loaded.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if path != loaded.root && isSkippedDir(path, entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		return addGeneratorUses(path, loaded.module, used)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read generators: %w", err)
	}

	return used, nil
}

// isSkippedDir says whether a directory holds nothing of the module's own.
func isSkippedDir(path, name string) bool {
	if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "testdata" {
		return true
	}

	_, err := os.Stat(filepath.Join(path, "go.mod"))

	return err == nil
}

func addGeneratorUses(path, module string, used map[string]bool) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	if !bytes.Contains(contents, ignoreConstraint) {
		return nil
	}

	file, err := parser.ParseFile(token.NewFileSet(), path, contents, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}

	if !isIgnored(file) {
		return nil
	}

	imported := map[string]bool{}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || (importPath != module && !strings.HasPrefix(importPath, module+"/")) {
			continue
		}

		name := filepath.Base(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
		}

		imported[name] = true
	}

	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if pkg, ok := selector.X.(*ast.Ident); ok && imported[pkg.Name] {
			used[selector.Sel.Name] = true
		}

		return true
	})

	return nil
}

// isIgnored says whether a file is built only under the ignore tag.
func isIgnored(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.Pos() >= file.Package {
			return false
		}

		for _, comment := range group.List {
			if comment.Text == string(ignoreConstraint) {
				return true
			}
		}
	}

	return false
}
