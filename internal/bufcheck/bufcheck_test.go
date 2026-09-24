package bufcheck_test

import (
	"fmt"
	"testing"

	"komrade/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

const testFile = "check/v1/check.proto"

// TestSpec checks the spec buf loads, which refuses a rule missing an ID, a purpose or a type.
func TestSpec(t *testing.T) {
	t.Parallel()

	checktest.SpecTest(t, bufcheck.Spec)
}

// TestRules pins which method names each rule reaches.
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
			name:   "a request's id fields refuse the empty string",
			ruleID: bufcheck.RequestIDRuleID,
			want: []checktest.ExpectedAnnotation{
				{
					RuleID:  bufcheck.RequestIDRuleID,
					Message: idMessage("id", "IDRequest", "GetThing"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   44,
						StartColumn: 2,
						EndLine:     44,
						EndColumn:   16,
					},
				},
				{
					RuleID:  bufcheck.RequestIDRuleID,
					Message: idMessage("agent_id", "IDRequest", "GetThing"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   45,
						StartColumn: 2,
						EndLine:     45,
						EndColumn:   22,
					},
				},
				{
					RuleID:  bufcheck.RequestIDRuleID,
					Message: idMessage("ids", "IDRequest", "GetThing"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   46,
						StartColumn: 2,
						EndLine:     46,
						EndColumn:   26,
					},
				},
				{
					RuleID:  bufcheck.RequestIDRuleID,
					Message: idMessage("agent_id", "Inner", "GetNested"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   75,
						StartColumn: 2,
						EndLine:     75,
						EndColumn:   22,
					},
				},
				{
					RuleID:  bufcheck.RequestIDRuleID,
					Message: idMessage("round_id", "Loop", "GetNested"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   82,
						StartColumn: 2,
						EndLine:     82,
						EndColumn:   22,
					},
				},
			},
		},
		{
			name:   "a request's enum fields refuse an undefined value",
			ruleID: bufcheck.RequestEnumRuleID,
			want: []checktest.ExpectedAnnotation{
				{
					RuleID:  bufcheck.RequestEnumRuleID,
					Message: enumMessage("colour", "EnumRequest", "(buf.validate.field).enum.defined_only = true", "GetEnum"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   63,
						StartColumn: 2,
						EndLine:     63,
						EndColumn:   20,
					},
				},
				{
					RuleID:  bufcheck.RequestEnumRuleID,
					Message: enumMessage("colours", "EnumRequest", "(buf.validate.field).repeated.items.enum.defined_only = true", "GetEnum"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   65,
						StartColumn: 2,
						EndLine:     65,
						EndColumn:   30,
					},
				},
				{
					RuleID:  bufcheck.RequestEnumRuleID,
					Message: enumMessage("tint", "Inner", "(buf.validate.field).enum.defined_only = true", "GetNested"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   76,
						StartColumn: 2,
						EndLine:     76,
						EndColumn:   18,
					},
				},
				{
					RuleID:  bufcheck.RequestEnumRuleID,
					Message: enumMessage("palette", "NestedRequest", "(buf.validate.field).map.values.enum.defined_only = true", "GetNested"),
					FileLocation: &checktest.ExpectedFileLocation{
						FileName:    testFile,
						StartLine:   87,
						StartColumn: 2,
						EndLine:     87,
						EndColumn:   34,
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

func idMessage(field string, message string, method string) string {
	return fmt.Sprintf(
		"%q names an id on %s, so it must carry a validation rule refusing it empty, such as (buf.validate.field).string.min_len = 1. The CLI marks a flag required and the MCP schema marks a field required from that rule, so without it both surfaces call the field optional while %s still needs it.",
		field,
		message,
		method,
	)
}

func enumMessage(field string, message string, spelling string, method string) string {
	return fmt.Sprintf(
		"%q on %s must carry %s. Without it a number the enum does not define reaches %s as a value no switch over it expects.",
		field,
		message,
		spelling,
		method,
	)
}

func message(method string, kind string, level string) string {
	return fmt.Sprintf(
		"%q is named for %s, so it must declare \"option idempotency_level = %s;\". The MCP tool hints derive from that option, so without it the method reaches every client as one that changes the panel and cannot be retried.",
		method,
		kind,
		level,
	)
}
