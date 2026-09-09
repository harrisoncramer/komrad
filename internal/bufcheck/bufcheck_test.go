package bufcheck_test

import (
	"fmt"
	"testing"

	"weave/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

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
				annotation(bufcheck.ReadRuleID, readMessage("GetBare"), 30, 30, 44),
				annotation(bufcheck.ReadRuleID, readMessage("GetWrong"), 35, 37, 3),
			},
		},
		{
			name:   "a repeatable write declares idempotent",
			ruleID: bufcheck.MutationRuleID,
			want: []checktest.ExpectedAnnotation{
				annotation(bufcheck.MutationRuleID, mutationMessage("SetBare"), 31, 31, 44),
				annotation(bufcheck.MutationRuleID, mutationMessage("SetWrong"), 38, 40, 3),
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
						FilePaths: []string{"check/v1/check.proto"},
					},
					RuleIDs: []string{tt.ruleID},
				},
				Spec:                bufcheck.Spec,
				ExpectedAnnotations: tt.want,
			}.Run(t)
		})
	}
}

func annotation(ruleID string, message string, startLine int, endLine int, endColumn int) checktest.ExpectedAnnotation {
	return checktest.ExpectedAnnotation{
		RuleID:  ruleID,
		Message: message,
		FileLocation: &checktest.ExpectedFileLocation{
			FileName:    "check/v1/check.proto",
			StartLine:   startLine,
			StartColumn: 2,
			EndLine:     endLine,
			EndColumn:   endColumn,
		},
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
