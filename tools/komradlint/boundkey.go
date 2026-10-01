package komradlint

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const boundKeyDoc = "Checks that a press is matched against an action key only in a screen's table, " +
	"a menu entry or a declared input path, so an action cannot be bound straight on a screen."

const (
	concatPath    = "slices.Concat"
	maxTraceDepth = 8
)

// boundKey is the rule's settings, read into sets once.
type boundKey struct {
	roots      map[string]string
	matchers   map[string]struct{}
	compares   map[string]struct{}
	navigation []string
	sources    map[string]struct{}
	paths      map[string]struct{}
}

// keyTracer follows key slices through one function body.
type keyTracer struct {
	rule    boundKey
	pass    *analysis.Pass
	defined map[types.Object]ast.Expr
}

// boundCall is one matcher call and the function it sits in.
type boundCall struct {
	matcher   string
	enclosing string
	strict    bool
}

// newBoundKey builds the rule that action keys are matched only where the menu or a declared path matches them.
func newBoundKey(settings config.BoundKeyConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	rule := boundKey{
		roots:      make(map[string]string, len(settings.Roots)),
		matchers:   setOf(settings.Matchers),
		compares:   setOf(settings.Compares),
		navigation: settings.Navigation,
		sources:    setOf(settings.Sources),
		paths:      setOf(slices.Concat(settings.Dispatchers, settings.Inputs)),
	}
	for _, root := range settings.Roots {
		rule.roots[root.Type] = root.Prefix
	}

	return &analysis.Analyzer{
		Name: "bound_key",
		Doc:  boundKeyDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isUnder(pass, file, excluded) {
					continue
				}

				for _, decl := range file.Decls {
					if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
						rule.reportFunc(pass, fn)
					}
				}
			}

			return nil, nil
		},
	}, nil
}

// reportFunc reports every press a function matches against a key it may not match.
func (b boundKey) reportFunc(pass *analysis.Pass, fn *ast.FuncDecl) {
	name := ""
	if obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func); ok {
		name = funcPath(obj)
	}

	if _, ok := b.paths[name]; ok {
		return
	}

	if _, ok := b.matchers[name]; ok {
		return
	}

	tracer := keyTracer{
		rule:    b,
		pass:    pass,
		defined: collectDefinitions(pass, fn.Body),
	}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		callee := resolveCallee(pass, call)
		if callee == nil {
			return true
		}

		_, strict := b.matchers[funcPath(callee)]
		_, loose := b.compares[funcPath(callee)]
		if !strict && !loose {
			return true
		}

		bound := boundCall{
			matcher:   callee.Name(),
			enclosing: name,
			strict:    strict,
		}
		for _, arg := range call.Args {
			if !isStringSlice(pass.TypesInfo.TypeOf(arg)) {
				continue
			}

			tracer.reportArg(bound, arg)
		}

		return true
	})
}

// reportArg reports a key slice handed to a matcher when it holds an action key, or when a strict matcher is handed one that cannot be traced.
func (t keyTracer) reportArg(call boundCall, arg ast.Expr) {
	keys, ok := t.trace(arg, 0)
	if !ok && !call.strict {
		return
	}

	if !ok {
		t.pass.Reportf(arg.Pos(), "%s matches keys that cannot be traced to the keybindings: match them in a table or a menu entry, or declare %s an input", call.matcher, call.enclosing)

		return
	}

	for _, key := range keys {
		if !t.rule.isNavigation(key) {
			t.pass.Reportf(arg.Pos(), "%s matches the action key %s outside a table or a menu entry: put it in the menu, or declare %s an input", call.matcher, key, call.enclosing)

			return
		}
	}
}

// trace is the key paths an expression holds, or false when it holds something the rule cannot follow.
func (t keyTracer) trace(expr ast.Expr, depth int) ([]string, bool) {
	if depth > maxTraceDepth {
		return nil, false
	}

	switch expr := ast.Unparen(expr).(type) {
	case *ast.SelectorExpr:
		path, ok := t.traceField(expr)
		if !ok {
			return nil, false
		}

		return []string{path}, true
	case *ast.Ident:
		value, ok := t.defined[t.pass.TypesInfo.Uses[expr]]
		if !ok {
			return nil, false
		}

		return t.trace(value, depth+1)
	case *ast.CallExpr:
		return t.traceCall(expr, depth)
	}

	return nil, false
}

// traceCall is the key paths a call returns, for a concatenation of keys or a navigation source.
func (t keyTracer) traceCall(call *ast.CallExpr, depth int) ([]string, bool) {
	callee := resolveCallee(t.pass, call)
	if callee == nil {
		return nil, false
	}

	path := funcPath(callee)
	if _, ok := t.rule.sources[path]; ok {
		return nil, true
	}

	if path != concatPath {
		return nil, false
	}

	var keys []string
	for _, arg := range call.Args {
		traced, ok := t.trace(arg, depth+1)
		if !ok {
			return nil, false
		}

		keys = append(keys, traced...)
	}

	return keys, true
}

// traceField is a selector's path below the root type it was reached from.
func (t keyTracer) traceField(sel *ast.SelectorExpr) (string, bool) {
	if named := namedOf(t.pass.TypesInfo.TypeOf(sel.X)); named != nil {
		if prefix, ok := t.rule.roots[typePath(named)]; ok {
			return joinPath(prefix, sel.Sel.Name), true
		}
	}

	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}

	path, ok := t.traceField(inner)
	if !ok {
		return "", false
	}

	return path + "." + sel.Sel.Name, true
}

// isNavigation says whether a key path is one any function may match.
func (b boundKey) isNavigation(key string) bool {
	for _, allowed := range b.navigation {
		if key == allowed || strings.HasPrefix(key, allowed+".") {
			return true
		}
	}

	return false
}

// collectDefinitions maps each variable a body declares with a value and never assigns again to that value.
func collectDefinitions(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]ast.Expr {
	defined := map[types.Object]ast.Expr{}
	reassigned := map[types.Object]struct{}{}
	ast.Inspect(body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}

		for i, lhs := range assign.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}

			if obj := pass.TypesInfo.Defs[ident]; obj != nil && assign.Tok == token.DEFINE && len(assign.Lhs) == len(assign.Rhs) {
				defined[obj] = assign.Rhs[i]

				continue
			}

			if obj := pass.TypesInfo.Uses[ident]; obj != nil {
				reassigned[obj] = struct{}{}
			}
		}

		return true
	})

	for obj := range reassigned {
		delete(defined, obj)
	}

	return defined
}

// resolveCallee is the function a call names, or nil for a call through a value.
func resolveCallee(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	var ident *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	case *ast.IndexExpr:
		if id, ok := fun.X.(*ast.Ident); ok {
			ident = id
		}
	}

	if ident == nil {
		return nil
	}

	fn, _ := pass.TypesInfo.Uses[ident].(*types.Func)

	return fn
}

// funcPath is a function spelled as its import path, a dot and its name, with a method's receiver type between them.
func funcPath(fn *types.Func) string {
	if fn.Pkg() == nil {
		return fn.Name()
	}

	sig, ok := fn.Type().(*types.Signature)
	if ok && sig.Recv() != nil {
		if named := namedOf(sig.Recv().Type()); named != nil {
			return typePath(named) + "." + fn.Name()
		}
	}

	return fn.Pkg().Path() + "." + fn.Name()
}

// isStringSlice says whether a type is a slice of strings, which is how a key's bindings are held.
func isStringSlice(typ types.Type) bool {
	slice, ok := types.Unalias(typ).(*types.Slice)
	if !ok {
		return false
	}

	basic, ok := slice.Elem().Underlying().(*types.Basic)

	return ok && basic.Kind() == types.String
}

// joinPath puts a root's prefix before a field name.
func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}

	return prefix + "." + name
}

// setOf is a list read into a set.
func setOf(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}

	return set
}
