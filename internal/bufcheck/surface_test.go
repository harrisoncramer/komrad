package bufcheck_test

import (
	"fmt"
	"testing"

	"weave/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

const surfaceFile = "surface/v1/surface.proto"

// TestSurfaces runs the methods declaring their surfaces consistently alongside each way of
// getting it wrong.
func TestSurfaces(t *testing.T) {
	t.Parallel()

	expect := func(_ string, message string, start, end int) checktest.ExpectedAnnotation {
		return checktest.ExpectedAnnotation{
			RuleID:  bufcheck.SurfacesRuleID,
			Message: message,
			FileLocation: &checktest.ExpectedFileLocation{
				FileName:    surfaceFile,
				StartLine:   start,
				StartColumn: 2,
				EndLine:     end,
				EndColumn:   endColumnOf(start, end),
			},
		}
	}

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata", "../../proto/proto"},
				FilePaths: []string{surfaceFile},
			},
			RuleIDs: []string{bufcheck.SurfacesRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			expect("Undeclared", undeclaredMessage("Undeclared"), 36, 36),
			expect("ModelOnly", modelOnlyMessage("ModelOnly"), 37, 45),
			expect("Nameless", namelessMessage("Nameless"), 46, 52),
			expect("Wordless", wordlessMessage("Wordless"), 53, 59),
			expect("Silent", silentMessage("Silent"), 60, 65),
			expect("Apologetic", apologeticMessage("Apologetic"), 66, 75),
			expect("Roleless", rolelessMessage("Roleless"), 76, 83),
		},
	}.Run(t)
}

// endColumnOf is where a method's declaration ends: the closing brace of a body, or the end of
// the one-line form.
func endColumnOf(start, end int) int {
	if start == end {
		return 47
	}

	return 3
}

func rolelessMessage(method string) string {
	return fmt.Sprintf(
		"%q is offered as a tool without naming the roles it is served to. A tool list is the largest fixed cost in an agent's context, so every tool says which of ROLE_IMPLEMENTER, ROLE_REVIEWER and ROLE_ORCHESTRATOR it is for; name all three where it is for all three.",
		method,
	)
}

func undeclaredMessage(method string) string {
	return fmt.Sprintf(
		"%q declares no (annotation.v1.surfaces), so nothing says who reaches it: the CLI and the MCP server are both generated from that annotation, and a method carrying none is served for the panel alone without saying so. Declare it, with cli and mcp each true or false.",
		method,
	)
}

func modelOnlyMessage(method string) string {
	return fmt.Sprintf(
		"%q is offered to a model as a tool but not to a script as a command. A tool is a command a model runs, so mcp implies cli; turn cli on or mcp off.",
		method,
	)
}

func namelessMessage(method string) string {
	return fmt.Sprintf(
		"%q is on a surface with no name, which is what the command and the tool are both generated under, so weave serves it on neither.",
		method,
	)
}

func wordlessMessage(method string) string {
	return fmt.Sprintf(
		"%q is on a surface with no description. That string is the whole of what a model is told about the call and all the help the CLI prints, so without it the method reaches its callers as a name and nothing else.",
		method,
	)
}

func silentMessage(method string) string {
	return fmt.Sprintf(
		"%q leaves a surface off with no reason. The reason is what tells the next reader a deliberate omission from an oversight.",
		method,
	)
}

func apologeticMessage(method string) string {
	return fmt.Sprintf(
		"%q is on every surface and yet gives a reason, which explains an omission that is not there. Drop the reason, or turn a surface off.",
		method,
	)
}
