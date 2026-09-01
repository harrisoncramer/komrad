// Package forbidnilnil refuses a nil pointer returned with a nil error, since the caller has
// been told nothing went wrong and is about to dereference it.
package forbidnilnil

import (
	"go/ast"
	"go/types"
	"strings"

	"agentslinter/config"

	"golang.org/x/tools/go/analysis"
)

const message = "do not return nil, nil; return a sentinel error the caller can match with errors.Is"

// NewAnalyzer returns the analyzer that flags a nil pointer returned alongside a nil error.
func NewAnalyzer(_ config.ForbidNilNilConfig) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:             "forbidnilnil",
		Doc:              "forbid returning a nil pointer with a nil error; say what happened with a sentinel error",
		RunDespiteErrors: true,
		Run:              run,
	}
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}

		ast.Inspect(file, func(node ast.Node) bool {
			switch fn := node.(type) {
			case *ast.FuncDecl:
				checkBody(pass, signatureOf(pass, fn.Name), fn.Body)
			case *ast.FuncLit:
				checkBody(pass, signatureOf(pass, fn), fn.Body)
			}

			return true
		})
	}

	return nil, nil
}

func signatureOf(pass *analysis.Pass, node ast.Expr) *types.Signature {
	var object types.Object
	if ident, ok := node.(*ast.Ident); ok {
		object = pass.TypesInfo.Defs[ident]
	}

	if object != nil {
		signature, _ := object.Type().(*types.Signature)
		return signature
	}

	signature, _ := pass.TypesInfo.TypeOf(node).(*types.Signature)
	return signature
}

// checkBody walks one function body, leaving the returns inside a nested literal to that literal.
func checkBody(pass *analysis.Pass, signature *types.Signature, body *ast.BlockStmt) {
	if body == nil || !returnsNilablePointerWithError(signature) {
		return
	}

	ast.Inspect(body, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if allNil(pass, stmt.Results) {
				pass.Report(analysis.Diagnostic{
					Pos:     stmt.Pos(),
					End:     stmt.End(),
					Message: message,
				})
			}
		}

		return true
	})
}

// returnsNilablePointerWithError holds the rule to pointers, where nil carries no meaning of its
// own, rather than to a nil map or slice that already reads as empty.
func returnsNilablePointerWithError(signature *types.Signature) bool {
	if signature == nil {
		return false
	}

	results := signature.Results()
	if results.Len() < 2 || !isError(results.At(results.Len()-1).Type()) {
		return false
	}

	for i := range results.Len() - 1 {
		if _, ok := results.At(i).Type().Underlying().(*types.Pointer); ok {
			return true
		}
	}

	return false
}

func isError(typ types.Type) bool {
	named, ok := typ.(*types.Named)
	return ok && named.Obj().Pkg() == nil && named.Obj().Name() == "error"
}

func allNil(pass *analysis.Pass, results []ast.Expr) bool {
	if len(results) == 0 {
		return false
	}

	for _, result := range results {
		ident, ok := result.(*ast.Ident)
		if !ok {
			return false
		}

		if _, ok := pass.TypesInfo.Uses[ident].(*types.Nil); !ok {
			return false
		}
	}

	return true
}
