package bufcheck_test

import (
	"testing"

	"komrad/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

// TestNotEmpty runs a file holding a filled message, an empty one, and an empty one nested.
func TestNotEmpty(t *testing.T) {
	t.Parallel()

	file := "empty/v1/empty.proto"

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata"},
				FilePaths: []string{file},
			},
			RuleIDs: []string{bufcheck.NotEmptyRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:  bufcheck.NotEmptyRuleID,
				Message: "empty.v1.HollowResponse declares no fields. Use google.protobuf.Empty instead.",
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:  file,
					StartLine: 10,
					EndLine:   10,
					EndColumn: 25,
				},
			},
			{
				RuleID:  bufcheck.NotEmptyRuleID,
				Message: "empty.v1.Outer.Inner declares no fields. Use google.protobuf.Empty instead.",
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:    file,
					StartLine:   13,
					StartColumn: 2,
					EndLine:     13,
					EndColumn:   18,
				},
			},
		},
	}.Run(t)
}
