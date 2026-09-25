package komradlint

import (
	"go/ast"
	"go/types"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const secretExposeDoc = "Checks that a forge Secret gives up its token only inside the functions " +
	"allowed to send or keep it, so a token cannot reach a log or an error by way of Expose."

// secretPackage is where the Secret type lives.
const secretPackage = "komrad/internal/forge"

// secretType is the type whose Expose method the rule watches.
const secretType = "Secret"

// newSecretExpose builds the rule that Expose is called only where the config allows.
func newSecretExpose(settings config.SecretExposeConfig) *analysis.Analyzer {
	allowed := make(map[string]struct{}, len(settings.Allow))
	for _, name := range settings.Allow {
		allowed[name] = struct{}{}
	}

	return &analysis.Analyzer{
		Name: "secret_expose",
		Doc:  secretExposeDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) {
					continue
				}

				reportExposes(pass, file, allowed)
			}

			return nil, nil
		},
	}
}

// reportExposes reports every Expose on a Secret whose enclosing declaration is not allowed,
// which for a package-level value is the package itself.
func reportExposes(pass *analysis.Pass, file *ast.File, allowed map[string]struct{}) {
	for _, decl := range file.Decls {
		body, name := scopeOf(pass, decl)
		if body == nil {
			continue
		}

		if _, ok := allowed[name]; ok {
			continue
		}

		ast.Inspect(body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isSecretExpose(pass, call) {
				return true
			}

			pass.Reportf(call.Pos(), "Expose gives up the token; %s is not a place allowed to send or keep it", name)

			return true
		})
	}
}

// scopeOf is what a declaration's Expose calls are judged as, a function by its full name and
// a package-level value by its package, and nothing for a declaration holding no code.
func scopeOf(pass *analysis.Pass, decl ast.Decl) (ast.Node, string) {
	if function, ok := decl.(*ast.FuncDecl); ok {
		if function.Body == nil {
			return nil, ""
		}

		return function.Body, fullName(pass, function)
	}

	if values, ok := decl.(*ast.GenDecl); ok {
		return values, pass.Pkg.Path()
	}

	return nil, ""
}

// fullName is the function's name as types spells it, such as (*pkg.T).M.
func fullName(pass *analysis.Pass, function *ast.FuncDecl) string {
	object, ok := pass.TypesInfo.Defs[function.Name].(*types.Func)
	if !ok {
		return function.Name.Name
	}

	return object.FullName()
}

// isSecretExpose says whether a call is Expose on the forge package's Secret.
func isSecretExpose(pass *analysis.Pass, call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Expose" {
		return false
	}

	method, ok := pass.TypesInfo.Uses[selector.Sel].(*types.Func)
	if !ok || method.Pkg() == nil || method.Pkg().Path() != secretPackage {
		return false
	}

	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return false
	}

	named, ok := types.Unalias(signature.Recv().Type()).(*types.Named)
	if !ok {
		return false
	}

	return named.Obj().Name() == secretType
}
