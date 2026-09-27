package bufcheck_test

import (
	"testing"

	"komrad/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

// TestFieldTypeNoStutter runs a file holding a field whose type repeats its name, a list of the
// same type, and a field named differently.
func TestFieldTypeNoStutter(t *testing.T) {
	t.Parallel()

	file := "stutter/v1/stutter.proto"

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata"},
				FilePaths: []string{file},
			},
			RuleIDs: []string{bufcheck.FieldTypeNoStutterRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:  bufcheck.FieldTypeNoStutterRuleID,
				Message: `"cat" on GetCatResponse is a CatRow, which declares a field named "cat" too. Flatten one into the other.`,
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:    file,
					StartLine:   14,
					StartColumn: 2,
					EndLine:     14,
					EndColumn:   17,
				},
			},
		},
	}.Run(t)
}
