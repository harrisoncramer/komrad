package bufcheck_test

import (
	"testing"

	"komrad/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

// TestGetReturnsResource runs a file holding a Get wrapping its resource in a row, one carrying it
// flat, and one whose noun names no message.
func TestGetReturnsResource(t *testing.T) {
	t.Parallel()

	file := "resource/v1/resource.proto"

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata"},
				FilePaths: []string{file},
			},
			RuleIDs: []string{bufcheck.GetReturnsResourceRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:  bufcheck.GetReturnsResourceRuleID,
				Message: `GetCatResponse must carry Cat as a field named "cat".`,
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:  file,
					StartLine: 19,
					EndLine:   21,
					EndColumn: 1,
				},
			},
		},
	}.Run(t)
}
