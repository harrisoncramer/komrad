package komradlint

import (
	"go/ast"
	"go/types"
	"regexp"
	"slices"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const storeQueryDoc = "Checks that a store query, a store package's function taking a context or a transaction, " +
	"is called only from the packages allowed to reach the database, so a handler or the panel reads through a service."

// newStoreQuery builds the rule that only the allowed packages call a store query.
func newStoreQuery(settings config.StoreQueryConfig) (*analysis.Analyzer, error) {
	allowed, err := compile(settings.Allow)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "store_query",
		Doc:  storeQueryDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip || isStorePackage(pass.Pkg) || isAllowedPackage(pass.Pkg.Path(), allowed) {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) {
					continue
				}

				reportStoreQueries(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportStoreQueries reports every use of a store query in a file.
func reportStoreQueries(pass *analysis.Pass, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}

		function, ok := pass.TypesInfo.Uses[ident].(*types.Func)
		if !ok || !isStoreQuery(function) {
			return true
		}

		pass.Reportf(ident.Pos(), "%s is a store query; %s reaches the database through a service", function.FullName(), pass.Pkg.Path())

		return true
	})
}

// isStoreQuery says whether a function is declared in a store package and takes a context or a transaction.
func isStoreQuery(function *types.Func) bool {
	return isStorePackage(function.Pkg()) && isQuerySignature(function)
}

// isQuerySignature says whether a function takes a context or a transaction.
func isQuerySignature(function *types.Func) bool {
	signature := function.Signature()

	for param := range signature.Params().Variables() {
		if isNamed(param.Type(), "context", "Context") || isTransaction(param.Type()) {
			return true
		}
	}

	return false
}

// isAllowedPackage says whether an import path matches any of the patterns, and never when there are none.
func isAllowedPackage(path string, patterns []*regexp.Regexp) bool {
	return slices.ContainsFunc(patterns, func(pattern *regexp.Regexp) bool { return pattern.MatchString(path) })
}
