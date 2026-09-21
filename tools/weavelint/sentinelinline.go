package weavelint

import (
	"go/ast"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const sentinelInlineDoc = "Checks that a fail constructor is called at package scope, " +
	"so every failure a caller can be answered with is a sentinel it can match on."

// failConstructors is the names in the fail package that declare a sentinel.
var failConstructors = map[string]bool{
	"Internal":        true,
	"NotFound":        true,
	"AlreadyExists":   true,
	"Precondition":    true,
	"InvalidArgument": true,
	"Exhausted":       true,
	"Denied":          true,
}

// newSentinelInline builds the rule that a fail constructor is not called inside a function.
func newSentinelInline(settings config.SentinelInlineConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "sentinel_inline",
		Doc:  sentinelInlineDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || isExcluded(pass, file, excluded) {
					continue
				}

				reportInlineSentinels(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportInlineSentinels reports every fail constructor called inside a function body.
func reportInlineSentinels(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}

		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}

			if name, ok := failConstructor(call); ok {
				pass.Reportf(call.Pos(),
					"fail.%s inside a function is a sentinel nobody can match: declare it as a package-level var and word it here with Saying", name)
			}

			return true
		})
	}
}

// failConstructor returns the name of the fail constructor a call invokes, if it invokes one.
func failConstructor(call *ast.CallExpr) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}

	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "fail" || !failConstructors[selector.Sel.Name] {
		return "", false
	}

	return selector.Sel.Name, true
}
