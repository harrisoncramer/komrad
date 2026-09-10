package bufcheck_test

import (
	"testing"

	"weave/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

// TestServiceName runs the clean files alongside the broken ones, since a rule reading the
// package name is as easy to write too strict as too loose.
func TestServiceName(t *testing.T) {
	t.Parallel()

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths: []string{"testdata"},
				FilePaths: []string{
					"okay/v1/okay.proto",
					"two_words/v1/two_words.proto",
					"misnamed/v1/misnamed.proto",
					"twin/v1/twin.proto",
				},
			},
			RuleIDs: []string{bufcheck.ServiceNameRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:  bufcheck.ServiceNameRuleID,
				Message: `the service in package misnamed.v1 should be named "MisnamedService" rather than "WrongService", so its name says which package answers a call.`,
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:  "misnamed/v1/misnamed.proto",
					StartLine: 8,
					EndLine:   10,
					EndColumn: 1,
				},
			},
			{
				RuleID:  bufcheck.ServiceNameRuleID,
				Message: "twin/v1/twin.proto declares 2 services, and weave's tooling reads the first one it finds. Give each service a file of its own.",
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:  "twin/v1/twin.proto",
					StartLine: 12,
					EndLine:   14,
					EndColumn: 1,
				},
			},
			{
				RuleID:  bufcheck.ServiceNameRuleID,
				Message: "twin/v1/twin.proto declares 2 services, and weave's tooling reads the first one it finds. Give each service a file of its own.",
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:  "twin/v1/twin.proto",
					StartLine: 16,
					EndLine:   18,
					EndColumn: 1,
				},
			},
		},
	}.Run(t)
}
