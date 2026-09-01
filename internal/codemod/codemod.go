// Package codemod is the shell the source rewriters under cmd share, walking a tree of Go
// files and handing each one to a rewrite that decides what the file should say instead.
package codemod

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
}

var generatedHeader = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

// Options is everything a run decides besides the rewrite itself.
type Options struct {
	Roots []string
	Tests bool
	Write bool
}

// Rewrite returns the source a file should hold instead, along with a count of what it
// changed, and returns a zero count when it would change nothing.
type Rewrite func(fset *token.FileSet, file *ast.File, src []byte) ([]byte, int, error)

// Result is what one rewrite did to one file.
type Result struct {
	Path  string
	Count int
}

// Run applies rewrite to every Go file under the roots, skipping generated files, and
// reports the files it changed.
func Run(opts Options, rewrite Rewrite) ([]Result, error) {
	roots := opts.Roots
	if len(roots) == 0 {
		roots = []string{"."}
	}

	var results []Result
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
			if !opts.Tests && strings.HasSuffix(path, "_test.go") {
				return nil
			}

			count, err := apply(path, opts.Write, rewrite)
			if err != nil {
				return err
			}
			if count > 0 {
				results = append(results, Result{
					Path:  path,
					Count: count,
				})
			}

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

func apply(path string, write bool, rewrite Rewrite) (int, error) {
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

	if IsGenerated(file) {
		return 0, nil
	}

	out, count, err := rewrite(fset, file, src)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	if count == 0 {
		return 0, nil
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

// IsGenerated reports whether the file carries the generated header above its package clause.
func IsGenerated(file *ast.File) bool {
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
