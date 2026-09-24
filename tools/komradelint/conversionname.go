package komradelint

import (
	"go/ast"
	"go/types"
	"regexp"
	"strings"

	"komradelint/config"

	"golang.org/x/tools/go/analysis"
)

const conversionNameDoc = "Checks that a helper converting to or from a proto message is named the way " +
	"its side of the wire names them: render on the way out of the service, From on the way into the panel."

// protoPackagePrefix is where every generated proto type lives.
const protoPackagePrefix = "komrade/proto/gen/"

// intoPanelSuffix is the name shape the panel's converters use.
const intoPanelSuffix = "From"

// newConversionName builds the rule that conversion helpers are named for their direction.
func newConversionName(settings config.ConversionNameConfig) (*analysis.Analyzer, error) {
	rendering, err := compile(settings.Render)
	if err != nil {
		return nil, err
	}

	converting, err := compile(settings.From)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "conversion_name",
		Doc:  conversionNameDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) {
					continue
				}

				if isUnder(pass, file, rendering) {
					reportUnrendered(pass, file)
				}

				if isUnder(pass, file, converting) {
					reportProtoNames(pass, file)
				}
			}

			return nil, nil
		},
	}, nil
}

// renderPackage is the package whose name already says render for every function in it.
const renderPackage = "render"

// reportUnrendered reports every function returning a proto type whose name does not say render.
func reportUnrendered(pass *analysis.Pass, file *ast.File) {
	if pass.Pkg.Name() == renderPackage {
		return
	}

	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv != nil || !returnsProto(pass, function) {
			continue
		}

		if strings.HasPrefix(function.Name.Name, "render") {
			continue
		}

		pass.Reportf(function.Pos(), "%s builds a proto message: name it render%s",
			function.Name.Name, renderedName(function.Name.Name))
	}
}

// protoNames matches the names the panel does not spell a converter with.
var protoNames = regexp.MustCompile(`^protoTo[A-Z]|FromProto$`)

// reportProtoNames reports every function named after the proto rather than for its direction.
func reportProtoNames(pass *analysis.Pass, file *ast.File) {
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv != nil {
			continue
		}

		name := function.Name.Name
		if strings.HasPrefix(name, "render") && takesProto(pass, function) {
			pass.Reportf(function.Pos(), "%s reads a proto message: name it %s",
				name, fromName(strings.TrimPrefix(name, "render")))

			continue
		}

		if !protoNames.MatchString(name) {
			continue
		}

		pass.Reportf(function.Pos(), "%s is named after the proto: name it %s",
			name, fromName(strings.TrimSuffix(strings.TrimPrefix(name, "protoTo"), "FromProto")))
	}
}

// toPrefix matches the name shape the render rule strips before it hints.
var toPrefix = regexp.MustCompile(`^to[A-Z]`)

// renderedName is the name a converter carries after render, its first letter raised.
func renderedName(name string) string {
	if toPrefix.MatchString(name) {
		name = strings.TrimPrefix(name, "to")
	}

	return strings.ToUpper(name[:1]) + name[1:]
}

// fromName is the panel's spelling of a converter, its first letter lowered.
func fromName(name string) string {
	if name == "" {
		return "x" + intoPanelSuffix
	}

	return strings.ToLower(name[:1]) + name[1:] + intoPanelSuffix
}

// returnsProto says whether any result of a function is a generated proto type.
func returnsProto(pass *analysis.Pass, function *ast.FuncDecl) bool {
	if function.Type.Results == nil {
		return false
	}

	for _, field := range function.Type.Results.List {
		if isProto(pass.TypesInfo.TypeOf(field.Type)) {
			return true
		}
	}

	return false
}

// takesProto says whether any parameter of a function is a generated proto type.
func takesProto(pass *analysis.Pass, function *ast.FuncDecl) bool {
	for _, field := range function.Type.Params.List {
		if isProto(pass.TypesInfo.TypeOf(field.Type)) {
			return true
		}
	}

	return false
}

// isProto says whether a type, or what it points to or holds, is declared under the generated
// proto packages.
func isProto(typ types.Type) bool {
	switch unwrapped := types.Unalias(typ).(type) {
	case *types.Pointer:
		return isProto(unwrapped.Elem())
	case *types.Slice:
		return isProto(unwrapped.Elem())
	case *types.Named:
		pkg := unwrapped.Obj().Pkg()

		return pkg != nil && strings.HasPrefix(pkg.Path(), protoPackagePrefix)
	default:
		return false
	}
}
