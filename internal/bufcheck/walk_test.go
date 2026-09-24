package bufcheck_test

import (
	"testing"

	"komrade/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

// TestImportedMessageIsNotWalked covers a request holding a message komrade does not declare, whose
// google.protobuf.Field carries two enum fields nothing here can annotate.
func TestImportedMessageIsNotWalked(t *testing.T) {
	t.Parallel()

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata"},
				FilePaths: []string{"check/v1/imported.proto"},
			},
			RuleIDs: []string{bufcheck.RequestEnumRuleID, bufcheck.RequestIDRuleID},
		},
		Spec: bufcheck.Spec,
	}.Run(t)
}
