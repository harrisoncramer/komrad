package weavelint

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const sentinelKindDoc = "Checks that a sentinel error declares the kind of failure it is, " +
	"so the code a caller is answered with is decided where the sentinel is written."

// newSentinelKind builds the rule that a package-scope sentinel names its kind.
func newSentinelKind(settings config.SentinelKindConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "sentinel_kind",
		Doc:  sentinelKindDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || isExcluded(pass, file, excluded) {
					continue
				}

				reportSentinels(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportSentinels reports every package-scope variable built by errors.New.
func reportSentinels(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		declaration, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		for _, spec := range declaration.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for _, expression := range value.Values {
				if !isErrorsNew(expression) {
					continue
				}

				pass.Reportf(expression.Pos(),
					"a sentinel here must say what kind of failure it is: use a fail constructor rather than errors.New")
			}
		}
	}
}

// isErrorsNew says whether an expression is a call to errors.New.
func isErrorsNew(expression ast.Expr) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return false
	}

	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	pkg, ok := selector.X.(*ast.Ident)

	return ok && pkg.Name == "errors" && selector.Sel.Name == "New"
}

// isTest says whether a file is a test, whose errors answer nobody.
func isTest(pass *analysis.Pass, file *ast.File) bool {
	return strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go")
}

// isExcluded says whether a file sits in a package the rule does not hold.
func isExcluded(pass *analysis.Pass, file *ast.File, excluded []*regexp.Regexp) bool {
	return isUnder(pass, file, excluded)
}

// isUnder says whether a file's path matches any of the patterns.
func isUnder(pass *analysis.Pass, file *ast.File, patterns []*regexp.Regexp) bool {
	path := repoRelative(pass.Fset.Position(file.Pos()).Filename)
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}

	return false
}

// repoRelative is the path the patterns are written against.
func repoRelative(path string) string {
	root, err := os.Getwd()
	if err != nil {
		return path
	}

	return strings.TrimPrefix(path, root+string(filepath.Separator))
}

// compile reads the configured patterns, naming the one that would not parse.
func compile(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile exclude pattern %q: %w", pattern, err)
		}

		compiled = append(compiled, expression)
	}

	return compiled, nil
}
