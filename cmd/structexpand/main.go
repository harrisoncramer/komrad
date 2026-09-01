// Command structexpand rewrites single-line keyed struct literals so each
// field sits on a line of its own.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"sort"

	"weave/internal/codemod"
)

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

	opts := codemod.Options{
		Roots: flag.Args(),
		Tests: *tests,
		Write: *write,
	}

	results, err := codemod.Run(opts, expander{min: *min}.rewrite)

	changed := 0
	for _, result := range results {
		changed += result.Count
		fmt.Printf("%s: %d literal(s)\n", result.Path, result.Count)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("%d literal(s) expanded\n", changed)
	if changed > 0 && !*write {
		os.Exit(1)
	}
}

type expander struct {
	min int
}

func (e expander) rewrite(fset *token.FileSet, file *ast.File, src []byte) ([]byte, int, error) {
	scan := scanner{
		fset: fset,
		file: file,
		min:  e.min,
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
		return nil, 0, nil
	}

	sort.Slice(inserts, func(i, j int) bool { return inserts[i].off > inserts[j].off })
	out := src
	for _, ins := range inserts {
		out = append(out[:ins.off:ins.off], append([]byte(ins.text), out[ins.off:]...)...)
	}

	return out, count, nil
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
