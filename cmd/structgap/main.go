// Command structgap closes the blank lines inside struct declarations and keyed struct
// literals, where a gap between fields reads as a grouping nobody meant.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strings"

	"weave/internal/codemod"
)

func main() {
	write := flag.Bool("w", false, "write results back to the files")
	tests := flag.Bool("tests", true, "include _test.go files")
	docs := flag.Bool("docs", true, "keep the blank line in front of a documented field")
	flag.Parse()

	opts := codemod.Options{
		Roots: flag.Args(),
		Tests: *tests,
		Write: *write,
	}

	closer := gapCloser{keepDocs: *docs}
	results, err := codemod.Run(opts, closer.rewrite)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	closed := 0
	for _, result := range results {
		closed += result.Count
		fmt.Printf("%s: %d gap(s)\n", result.Path, result.Count)
	}

	fmt.Printf("%d gap(s) closed\n", closed)
	if closed > 0 && !*write {
		os.Exit(1)
	}
}

type gapCloser struct {
	keepDocs bool
}

func (g gapCloser) rewrite(fset *token.FileSet, file *ast.File, src []byte) ([]byte, int, error) {
	scan := gapScan{
		closer: g,
		fset:   fset,
		lines:  strings.Split(string(src), "\n"),
		drop:   make(map[int]bool),
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.StructType:
			scan.structType(node)
		case *ast.CompositeLit:
			scan.literal(node)
		}

		return true
	})

	if scan.count == 0 {
		return nil, 0, nil
	}

	var kept []string
	for i, line := range scan.lines {
		if !scan.drop[i+1] {
			kept = append(kept, line)
		}
	}

	return []byte(strings.Join(kept, "\n")), scan.count, nil
}

type gapScan struct {
	closer gapCloser
	fset   *token.FileSet
	lines  []string
	drop   map[int]bool
	count  int
}

func (s *gapScan) structType(st *ast.StructType) {
	if st.Fields == nil || !st.Fields.Opening.IsValid() || !st.Fields.Closing.IsValid() {
		return
	}

	prev := st.Fields.Opening
	for i, field := range st.Fields.List {
		start := field.Pos()
		documented := field.Doc != nil
		if documented {
			start = field.Doc.Pos()
		}
		s.close(prev, start, documented && i > 0)
		prev = field.End()
	}
	s.close(prev, st.Fields.Closing, false)
}

func (s *gapScan) literal(lit *ast.CompositeLit) {
	if !isKeyedStruct(lit) {
		return
	}

	prev := lit.Lbrace
	for _, elt := range lit.Elts {
		s.close(prev, elt.Pos(), false)
		prev = elt.End()
	}
	s.close(prev, lit.Rbrace, false)
}

func (s *gapScan) close(prev, next token.Pos, documented bool) {
	if documented && s.closer.keepDocs {
		return
	}

	from := s.fset.Position(prev).Line
	to := s.fset.Position(next).Line

	var blanks []int
	for line := from + 1; line < to; line++ {
		if strings.TrimSpace(s.lines[line-1]) != "" {
			return
		}
		blanks = append(blanks, line)
	}
	if len(blanks) == 0 {
		return
	}

	for _, line := range blanks {
		s.drop[line] = true
	}
	s.count++
}

func isKeyedStruct(lit *ast.CompositeLit) bool {
	if len(lit.Elts) == 0 || !lit.Lbrace.IsValid() || !lit.Rbrace.IsValid() {
		return false
	}
	switch lit.Type.(type) {
	case nil, *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
	default:
		return false
	}
	for _, elt := range lit.Elts {
		if _, ok := elt.(*ast.KeyValueExpr); !ok {
			return false
		}
	}

	return true
}
