package bufcheck_test

import (
	"testing"

	"weave/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

const uniqueFile = "check/v1/unique.proto"

// TestTypesUnique pins that google.protobuf.Empty is the one type two methods may share, since
// the rule exists to allow it where buf's own cannot.
func TestTypesUnique(t *testing.T) {
	t.Parallel()

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata"},
				FilePaths: []string{uniqueFile},
			},
			RuleIDs: []string{bufcheck.TypesUniqueRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:  bufcheck.TypesUniqueRuleID,
				Message: sharedMessage("SetOne", "SetTwo"),
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:    uniqueFile,
					StartLine:   20,
					StartColumn: 2,
					EndLine:     22,
					EndColumn:   3,
				},
			},
			{
				RuleID:  bufcheck.TypesUniqueRuleID,
				Message: sharedMessage("SetTwo", "SetOne"),
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:    uniqueFile,
					StartLine:   23,
					StartColumn: 2,
					EndLine:     25,
					EndColumn:   3,
				},
			},
			{
				RuleID:  bufcheck.TypesUniqueRuleID,
				Message: "\"GetRoundtrip\" takes and returns the same type \"check.v1.Roundtrip\", so neither can gain a field without changing the other.",
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:    uniqueFile,
					StartLine:   26,
					StartColumn: 2,
					EndLine:     28,
					EndColumn:   3,
				},
			},
			{
				RuleID:  bufcheck.TypesUniqueRuleID,
				Message: "\"GetAlso\" shares the type \"check.v1.Roundtrip\" with check.v1.UniqueService.GetRoundtrip, so a field one of them needs is a field the others grow too. Give each method its own type, or return google.protobuf.Empty where there is nothing to say.",
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:    uniqueFile,
					StartLine:   29,
					StartColumn: 2,
					EndLine:     31,
					EndColumn:   3,
				},
			},
		},
	}.Run(t)
}

func sharedMessage(method string, other string) string {
	return "\"" + method + "\" shares the type \"check.v1.SharedRequest\" with check.v1.UniqueService." + other +
		", so a field one of them needs is a field the others grow too. Give each method its own type, or return google.protobuf.Empty where there is nothing to say."
}
