package bufcheck_test

import (
	"testing"

	"weave/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

// TestOneService runs the file holding one service alongside the file holding two, since a rule
// counting them is as easy to write too strict as too loose.
func TestOneService(t *testing.T) {
	t.Parallel()

	twinFile := "twin/v1/twin.proto"
	shared := "twin/v1/twin.proto declares 2 services, and weave's tooling reads the first one it finds. Give each service a file of its own."

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata"},
				FilePaths: []string{"check/v1/unique.proto", twinFile},
			},
			RuleIDs: []string{bufcheck.OneServiceRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:  bufcheck.OneServiceRuleID,
				Message: shared,
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:  twinFile,
					StartLine: 12,
					EndLine:   14,
					EndColumn: 1,
				},
			},
			{
				RuleID:  bufcheck.OneServiceRuleID,
				Message: shared,
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:  twinFile,
					StartLine: 16,
					EndLine:   18,
					EndColumn: 1,
				},
			},
		},
	}.Run(t)
}
