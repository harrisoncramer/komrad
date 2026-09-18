package bufcheck_test

import (
	"fmt"
	"testing"

	"weave/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

const surfaceFile = "surface/v1/surface.proto"

// TestOneSurface runs the methods declaring one surface alongside those declaring none or both.
func TestOneSurface(t *testing.T) {
	t.Parallel()

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata", "../../proto/proto"},
				FilePaths: []string{surfaceFile},
			},
			RuleIDs: []string{bufcheck.OneSurfaceRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:  bufcheck.OneSurfaceRuleID,
				Message: neitherMessage("Neither"),
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:    surfaceFile,
					StartLine:   20,
					StartColumn: 2,
					EndLine:     20,
					EndColumn:   44,
				},
			},
			{
				RuleID:  bufcheck.OneSurfaceRuleID,
				Message: bothMessage("Both"),
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:    surfaceFile,
					StartLine:   21,
					StartColumn: 2,
					EndLine:     27,
					EndColumn:   3,
				},
			},
			{
				RuleID:  bufcheck.OneSurfaceRuleID,
				Message: silentMessage("Silent"),
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:    surfaceFile,
					StartLine:   28,
					StartColumn: 2,
					EndLine:     30,
					EndColumn:   3,
				},
			},
		},
	}.Run(t)
}

func neitherMessage(method string) string {
	return fmt.Sprintf(
		"%q declares neither (annotation.v1.tool) nor (annotation.v1.panel_only), so it is served and reachable from nothing: the CLI and the MCP server are both generated from the tool annotation, and a method carrying none exists for no caller. Give it a tool annotation, or say with panel_only why the panel is to keep it.",
		method,
	)
}

func bothMessage(method string) string {
	return fmt.Sprintf(
		"%q declares both (annotation.v1.tool) and (annotation.v1.panel_only), which say opposite things about the same method. Drop whichever of the two is wrong.",
		method,
	)
}

func silentMessage(method string) string {
	return fmt.Sprintf(
		"%q declares (annotation.v1.panel_only) with no reason. The reason is the whole of what the annotation records, so without it the next reader cannot tell a deliberate omission from an oversight.",
		method,
	)
}
