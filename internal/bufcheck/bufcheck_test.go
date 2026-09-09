package bufcheck_test

import (
	"fmt"
	"testing"

	"weave/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

const testFile = "check/v1/check.proto"

// TestSpec checks the spec buf loads, which refuses a rule missing an ID, a purpose or a type.
func TestSpec(t *testing.T) {
	t.Parallel()

	checktest.SpecTest(t, bufcheck.Spec)
}

// TestRules pins which method names each rule reaches, since a prefix that matched a word
// beginning with it would fail an RPC named for something else entirely.
func TestRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ruleID string
		want   []checktest.ExpectedAnnotation
	}{
		{
			name:   "a read declares no side effects",
			ruleID: bufcheck.ReadRuleID,
			want: []checktest.ExpectedAnnotation{
				{
					RuleID:  bufcheck.ReadRuleID,
					Message: readMessage("GetBare"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   30,
						StartColumn: 2,
						EndLine:     30,
						EndColumn:   44,
					},
				},
				{
					RuleID:  bufcheck.ReadRuleID,
					Message: readMessage("GetWrong"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   35,
						StartColumn: 2,
						EndLine:     37,
						EndColumn:   3,
					},
				},
			},
		},
		{
			name:   "a repeatable write declares idempotent",
			ruleID: bufcheck.MutationRuleID,
			want: []checktest.ExpectedAnnotation{
				{
					RuleID:  bufcheck.MutationRuleID,
					Message: mutationMessage("SetBare"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   31,
						StartColumn: 2,
						EndLine:     31,
						EndColumn:   44,
					},
				},
				{
					RuleID:  bufcheck.MutationRuleID,
					Message: mutationMessage("SetWrong"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   38,
						StartColumn: 2,
						EndLine:     40,
						EndColumn:   3,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checktest.CheckTest{
				Request: &checktest.RequestSpec{
					Files: &checktest.ProtoFileSpec{
						DirPaths:  []string{"testdata"},
						FilePaths: []string{testFile},
					},
					RuleIDs: []string{tt.ruleID},
				},
				Spec:                bufcheck.Spec,
				ExpectedAnnotations: tt.want,
			}.Run(t)
		})
	}
}

func readMessage(method string) string {
	return message(method, "a read", "NO_SIDE_EFFECTS")
}

func mutationMessage(method string) string {
	return message(method, "a write that can be repeated", "IDEMPOTENT")
}

func message(method string, kind string, level string) string {
	return fmt.Sprintf(
		"%q is named for %s, so it must declare \"option idempotency_level = %s;\". The MCP hints and the CLI both read that option, and without it the method reaches every client as one that mutates the panel and cannot be retried.",
		method,
		kind,
		level,
	)
}
