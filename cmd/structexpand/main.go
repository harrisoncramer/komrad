// Command structexpand rewrites single-line keyed struct literals so each
// field sits on a line of its own.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
}

var generatedHeader = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

type insert struct {
	off  int
	text string
}

type scanner struct {
	fset *token.FileSet
	file *ast.File
	min  int
}

func main() {
	min := flag.Int("min", 3, "expand literals with at least this many keyed fields")
	write := flag.Bool("w", false, "write results back to the files")
	tests := flag.Bool("tests", true, "include _test.go files")
	flag.Parse()

	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}

	changed := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDirs[d.Name()] {
					return fs.SkipDir
				}

				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if !*tests && strings.HasSuffix(path, "_test.go") {
				return nil
			}

			n, err := process(path, *min, *write)
			if err != nil {
				return err
			}
			if n > 0 {
				changed += n
				fmt.Printf("%s: %d literal(s)\n", path, n)
			}

			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	fmt.Printf("%d literal(s) expanded\n", changed)
	if changed > 0 && !*write {
		os.Exit(1)
	}
}

func process(path string, min int, write bool) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	src, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}

	if isGenerated(file) {
		return 0, nil
	}

	scan := scanner{
		fset: fset,
		file: file,
		min:  min,
	}

	var inserts []insert
	count := 0
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !scan.shouldExpand(lit) {
			return true
		}

		count++
		for _, elt := range lit.Elts {
			inserts = append(inserts, insert{
				off:  fset.Position(elt.Pos()).Offset,
				text: "\n",
			})
		}
		inserts = append(inserts, insert{
			off:  fset.Position(lit.Rbrace).Offset,
			text: ",\n",
		})

		return true
	})
	if count == 0 {
		return 0, nil
	}

	sort.Slice(inserts, func(i, j int) bool { return inserts[i].off > inserts[j].off })
	out := src
	for _, ins := range inserts {
		out = append(out[:ins.off:ins.off], append([]byte(ins.text), out[ins.off:]...)...)
	}

	formatted, err := format.Source(out)
	if err != nil {
		return 0, fmt.Errorf("%s: rewrite did not parse: %w", path, err)
	}

	if write {
		if err := os.WriteFile(path, formatted, info.Mode().Perm()); err != nil {
			return 0, err
		}
	}

	return count, nil
}

func (s scanner) shouldExpand(lit *ast.CompositeLit) bool {
	if len(lit.Elts) < s.min || !isStructLiteral(lit) {
		return false
	}
	if s.fset.Position(lit.Lbrace).Line != s.fset.Position(lit.Rbrace).Line {
		return false
	}
	for _, elt := range lit.Elts {
		if _, ok := elt.(*ast.KeyValueExpr); !ok {
			return false
		}
	}
	for _, group := range s.file.Comments {
		if group.Pos() > lit.Lbrace && group.End() <= lit.Rbrace {
			return false
		}
	}

	return true
}

func isStructLiteral(lit *ast.CompositeLit) bool {
	switch lit.Type.(type) {
	case nil, *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
		return true
	default:
		return false
	}
}

func isGenerated(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			return false
		}
		for _, line := range group.List {
			if generatedHeader.MatchString(line.Text) {
				return true
			}
		}
	}

	return false
}
