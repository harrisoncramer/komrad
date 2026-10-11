package komradlint

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const oneServiceDoc = "Checks that an RPC handler's strict half reaches one of its handler's fields, " +
	"so combining what several services return is done by a service rather than by the handler."

const (
	// failPackage is where the error a strict half answers with is declared.
	failPackage = "komrad/internal/fail"
	// failType is the name of that error.
	failType = "Error"
)

// handlerScope is what a package's strict halves are read against: every method it declares, and
// the fields a strict half may reach besides its one service.
type handlerScope struct {
	methods map[*types.Func]*ast.FuncDecl
	readers []string
}

// newOneService builds the rule that a strict half hands its request to one service.
func newOneService(settings config.OneServiceConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "one_service",
		Doc:  oneServiceDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			scope := handlerScope{methods: collectMethods(pass), readers: settings.Readers}
			for _, file := range pass.Files {
				if isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				reportServices(pass, file, scope)
			}

			return nil, nil
		},
	}, nil
}

// reportServices reports every strict half in a file that reaches more than one of its handler's fields besides the readers.
func reportServices(pass *analysis.Pass, file *ast.File, scope handlerScope) {
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv == nil || !function.Name.IsExported() {
			continue
		}

		method, ok := pass.TypesInfo.Defs[function.Name].(*types.Func)
		if !ok || !isStrictHalf(method) {
			continue
		}

		fields := collectFields(pass, scope.methods, function, map[*types.Func]struct{}{})
		for _, reader := range scope.readers {
			delete(fields, reader)
		}

		if len(fields) < 2 {
			continue
		}

		names := make([]string, 0, len(fields))
		for name := range fields {
			names = append(names, name)
		}
		slices.Sort(names)

		pass.Reportf(function.Name.Pos(), "%s reaches %s; give one service a method that does the combining", function.Name.Name, strings.Join(names, ", "))
	}
}

// isStrictHalf says whether a method answers with the error a strict half returns.
func isStrictHalf(method *types.Func) bool {
	results := method.Signature().Results()
	if results.Len() == 0 {
		return false
	}

	pointer, ok := results.At(results.Len() - 1).Type().(*types.Pointer)

	return ok && isNamed(pointer.Elem(), failPackage, failType)
}

// collectMethods maps every method the package declares outside its tests to its declaration.
func collectMethods(pass *analysis.Pass) map[*types.Func]*ast.FuncDecl {
	methods := map[*types.Func]*ast.FuncDecl{}
	for _, file := range pass.Files {
		if isTest(pass, file) {
			continue
		}

		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Body == nil {
				continue
			}

			if method, ok := pass.TypesInfo.Defs[function.Name].(*types.Func); ok {
				methods[method] = function
			}
		}
	}

	return methods
}

// collectFields is the names of the receiver's fields a method reaches, itself or through the receiver's other methods.
func collectFields(pass *analysis.Pass, methods map[*types.Func]*ast.FuncDecl, function *ast.FuncDecl, visited map[*types.Func]struct{}) map[string]struct{} {
	fields := map[string]struct{}{}
	receiver := receiverOf(pass, function)
	if receiver == nil || function.Body == nil {
		return fields
	}

	ast.Inspect(function.Body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		ident, ok := ast.Unparen(selector.X).(*ast.Ident)
		if !ok || pass.TypesInfo.Uses[ident] != receiver {
			return true
		}

		selection := pass.TypesInfo.Selections[selector]
		if selection == nil {
			return true
		}

		if selection.Kind() == types.FieldVal {
			if types.IsInterface(selection.Type()) {
				fields[selector.Sel.Name] = struct{}{}
			}

			return true
		}

		method, ok := selection.Obj().(*types.Func)
		if !ok {
			return true
		}

		if _, seen := visited[method]; seen {
			return true
		}

		visited[method] = struct{}{}
		if declared, ok := methods[method]; ok {
			for name := range collectFields(pass, methods, declared, visited) {
				fields[name] = struct{}{}
			}
		}

		return true
	})

	return fields
}

// receiverOf is the variable a method names its receiver, or nil when it leaves it unnamed.
func receiverOf(pass *analysis.Pass, function *ast.FuncDecl) types.Object {
	names := function.Recv.List[0].Names
	if len(names) == 0 {
		return nil
	}

	return pass.TypesInfo.Defs[names[0]]
}
