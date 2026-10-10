package komradlint

import (
	"go/ast"
	"go/token"
	"go/types"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const txStoreDoc = "Checks that a function handed a transaction queries only through stores built over it, " +
	"so no query inside it slips out onto the pool and commits apart from the rest."

const (
	// modelsPackage is where sqlc declares the transaction type every store is built over.
	modelsPackage = "komrad/db/models"
	// transactionType is the name sqlc gives that type.
	transactionType = "DBTX"
	// bindMethod is the method a service offers to rebuild itself over a transaction.
	bindMethod = "InTx"
)

// txScope is a function handed a transaction, spanning its parameters and body, which leaves out a method's receiver.
type txScope struct {
	start token.Pos
	end   token.Pos
}

// newTxStore builds the rule that a function holding a transaction queries only through it.
func newTxStore(settings config.TxStoreConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "tx_store",
		Doc:  txStoreDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				ast.Inspect(file, func(node ast.Node) bool {
					signature, body := splitFunction(node)
					if body != nil && takesTransaction(pass, signature) {
						reportEscapes(pass, txScope{start: signature.Params.Pos(), end: body.End()}, body)
					}

					return true
				})
			}

			return nil, nil
		},
	}, nil
}

// splitFunction is a declared or literal function's signature and body, or nothing for any other node.
func splitFunction(node ast.Node) (*ast.FuncType, *ast.BlockStmt) {
	switch function := node.(type) {
	case *ast.FuncDecl:
		return function.Type, function.Body
	case *ast.FuncLit:
		return function.Type, function.Body
	}

	return nil, nil
}

// takesTransaction says whether a function has a parameter that is a transaction.
func takesTransaction(pass *analysis.Pass, signature *ast.FuncType) bool {
	for _, field := range signature.Params.List {
		if isTransaction(pass.TypesInfo.TypeOf(field.Type)) {
			return true
		}
	}

	return false
}

// isTransaction says whether a type is sqlc's transaction, under any alias.
func isTransaction(typ types.Type) bool {
	return typ != nil && isNamed(types.Unalias(typ), modelsPackage, transactionType)
}

// reportEscapes reports every call in a transaction's function on a store or a bindable service
// built outside it, and every call it hands a transaction from outside it.
func reportEscapes(pass *analysis.Pass, scope txScope, body *ast.BlockStmt) {
	ast.Inspect(body, func(node ast.Node) bool {
		if literal, ok := node.(*ast.FuncLit); ok && takesTransaction(pass, literal.Type) {
			return false
		}

		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		callee := resolveCallee(pass, call)
		if callee == nil {
			return true
		}

		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && isQueryingReceiver(pass, callee, selector) && scope.isOutside(pass, receiverRoot(selector.X)) {
			pass.Reportf(call.Pos(), "%s runs outside the transaction this function holds; call it on a store built over the transaction", types.ExprString(call.Fun))

			return true
		}

		for _, arg := range call.Args {
			if isTransaction(pass.TypesInfo.TypeOf(arg)) && scope.isOutside(pass, valueRoot(arg)) {
				pass.Reportf(arg.Pos(), "%s is handed a transaction this function was not handed; hand it its own", types.ExprString(call.Fun))
			}
		}

		return true
	})
}

// isQueryingReceiver says whether a method call queries through its receiver: a query on a store
// package's type, or one other than InTx on a service that can be rebuilt over a transaction.
func isQueryingReceiver(pass *analysis.Pass, callee *types.Func, selector *ast.SelectorExpr) bool {
	signature, ok := callee.Type().(*types.Signature)
	if !ok || signature.Recv() == nil || !isQuerySignature(callee) {
		return false
	}

	if isStorePackage(callee.Pkg()) {
		return true
	}

	return callee.Name() != bindMethod && isBindable(pass.TypesInfo.TypeOf(selector.X))
}

// isBindable says whether a type has an InTx method taking a transaction.
func isBindable(typ types.Type) bool {
	if typ == nil {
		return false
	}

	object, _, _ := types.LookupFieldOrMethod(typ, true, nil, bindMethod)
	method, ok := object.(*types.Func)
	if !ok {
		return false
	}

	signature, ok := method.Type().(*types.Signature)

	return ok && signature.Params().Len() == 1 && isTransaction(signature.Params().At(0).Type())
}

// isOutside says whether a name is declared outside the scope, which a missing name or a package never is.
func (s txScope) isOutside(pass *analysis.Pass, root *ast.Ident) bool {
	if root == nil {
		return false
	}

	object := pass.TypesInfo.Uses[root]
	if object == nil {
		return false
	}

	if _, ok := object.(*types.PkgName); ok {
		return false
	}

	return object.Pos() < s.start || object.Pos() > s.end
}

// receiverRoot is the name a chain of selectors starts from, or nil when it starts from a call or a literal.
func receiverRoot(expr ast.Expr) *ast.Ident {
	for {
		selector, ok := ast.Unparen(expr).(*ast.SelectorExpr)
		if !ok {
			break
		}

		expr = selector.X
	}

	ident, ok := ast.Unparen(expr).(*ast.Ident)
	if !ok {
		return nil
	}

	return ident
}

// valueRoot is the name a value starts from, following a method call back to its receiver, or nil when it starts from a plain call or a literal.
func valueRoot(expr ast.Expr) *ast.Ident {
	for {
		call, ok := ast.Unparen(expr).(*ast.CallExpr)
		if !ok {
			return receiverRoot(expr)
		}

		selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			return nil
		}

		expr = selector.X
	}
}
