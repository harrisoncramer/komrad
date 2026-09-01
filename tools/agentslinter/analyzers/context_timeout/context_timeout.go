// Package contexttimeout asks the panel to bound the contexts it starts, since the panel talks
// to the service over HTTP and a call of its own that never times out is a frame that never
// redraws.
package contexttimeout

import (
	"errors"
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"regexp"

	"agentslinter/config"

	"golang.org/x/tools/go/analysis"
)

// NewAnalyzer builds the context-timeout analyzer, which holds a context started from nothing
// to being started with a named timeout so a call cannot wait on a dead server forever.
func NewAnalyzer(cfg config.ContextTimeoutConfig) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "contexttimeout",
		Doc:  "Checks that a context started from context.Background carries a timeout named by a constant",
		Run: func(pass *analysis.Pass) (any, error) {
			if len(cfg.Include) == 0 {
				return nil, errors.New("context_timeout: include must name at least one path pattern, since the rule reaches nothing without one")
			}

			include, err := compileIncludes(cfg.Include)
			if err != nil {
				return nil, err
			}

			for _, file := range pass.Files {
				if !isIncluded(include, pass.Fset.Position(file.Pos()).Filename) {
					continue
				}
				check(pass, file)
			}

			return nil, nil
		},
	}
}

func check(pass *analysis.Pass, file *ast.File) {
	bounded := map[ast.Node]struct{}{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isContextCall(pass, call, "WithTimeout") {
			return true
		}

		if len(call.Args) == 2 {
			bounded[call.Args[0]] = struct{}{}
			reportUnnamedTimeout(pass, call.Args[1])
		}

		return true
	})

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		name := ""
		switch {
		case isContextCall(pass, call, "Background"):
			name = "Background"
		case isContextCall(pass, call, "TODO"):
			name = "TODO"
		default:
			return true
		}

		if _, ok := bounded[ast.Node(call)]; !ok {
			pass.Report(analysis.Diagnostic{
				Pos:     call.Pos(),
				End:     call.End(),
				Message: fmt.Sprintf("context.%s must be started with context.WithTimeout, so the call cannot wait forever", name),
			})
		}

		return true
	})
}

// reportUnnamedTimeout flags a duration written where the call is, since a constant beside the
// others is what makes the timeouts of a package readable together.
func reportUnnamedTimeout(pass *analysis.Pass, arg ast.Expr) {
	switch expr := arg.(type) {
	case *ast.Ident:
		return
	case *ast.SelectorExpr:
		// A constant from another package is named; time.Minute is the duration itself.
		if !isPackageMember(pass, expr, "time") {
			return
		}
	}

	pass.Report(analysis.Diagnostic{
		Pos:     arg.Pos(),
		End:     arg.End(),
		Message: "the timeout must be a named constant rather than a duration written inline",
	})
}

func isContextCall(pass *analysis.Pass, call *ast.CallExpr, name string) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}

	return isPackageMember(pass, selector, "context")
}

func isPackageMember(pass *analysis.Pass, selector *ast.SelectorExpr, path string) bool {
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}

	pkg, ok := pass.TypesInfo.Uses[ident].(*types.PkgName)

	return ok && pkg.Imported().Path() == path
}

func compileIncludes(patterns []string) ([]*regexp.Regexp, error) {
	var compiled []*regexp.Regexp
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("context_timeout: include pattern %q: %w", pattern, err)
		}
		compiled = append(compiled, re)
	}

	return compiled, nil
}

func isIncluded(patterns []*regexp.Regexp, name string) bool {
	path := filepath.ToSlash(name)
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}

	return false
}
