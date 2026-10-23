package rpcapi

import (
	"encoding/json"
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestGetConstraints checks each validation rule reads as its JSON Schema keyword, on a value and on a list's items.
func TestGetConstraints(t *testing.T) {
	createAgent := (&agentv1.CreateAgentRequest{}).ProtoReflect().Descriptor()

	tests := []struct {
		message protoreflect.MessageDescriptor
		field   string
		want    string
	}{
		{message: createAgent, field: "prompt", want: `{"minLength":1,"maxLength":50000}`},
		{message: createAgent, field: "harness", want: `{"maxLength":40,"pattern":"^[a-z0-9-]*$"}`},
		{message: createAgent, field: "workflow_id", want: `{"format":"uuid"}`},
		{message: createAgent, field: "title", want: `{"maxLength":200,"rules":["must not contain control characters"]}`},
		{message: createAgent, field: "provision_enabled", want: `{}`},
		{
			message: (&agentv1.GetLastTurnsRequest{}).ProtoReflect().Descriptor(),
			field:   "turns",
			want:    `{"minimum":1,"maximum":20}`,
		},
		{
			message: (&agentv1.GetDocumentRequest{}).ProtoReflect().Descriptor(),
			field:   "version",
			want:    `{"exclusiveMinimum":0}`,
		},
		{
			message: (&agentv1.ArchiveAgentsRequest{}).ProtoReflect().Descriptor(),
			field:   "ids",
			want:    `{"minItems":1,"uniqueItems":true,"items":{"format":"uuid"}}`,
		},
		{
			message: (&agentv1.WriteDocumentRequest{}).ProtoReflect().Descriptor(),
			field:   "body",
			want:    `{"minLength":1,"maxBytes":1048576}`,
		},
		{
			message: (&workflowv1.WorkflowEventPrompt{}).ProtoReflect().Descriptor(),
			field:   "event",
			want: `{"enum": [
				"pull_request.checks_failing_running",
				"pull_request.checks_failing",
				"pull_request.checks_passing",
				"pull_request.changes_requested",
				"pull_request.approved",
				"pull_request.conflicting",
				"pull_request.behind",
				"pull_request.merged",
				"pull_request.closed",
				"pull_request.auto_merge_on",
				"worktree.rebased",
				"worktree.landed",
				"worktree.rebase_conflicted",
				"document.written_by_person",
				"review.approved",
				"review.changes_requested",
				"review.commented",
				"review.commented_by_person",
				"review.forge_commented",
				"review.answered",
				"review.forge_reply_requested",
				"review.forge_reply_decided"
			]}`,
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.message.Name())+"."+tt.field, func(t *testing.T) {
			field := tt.message.Fields().ByName(protoreflect.Name(tt.field))
			require.NotNil(t, field)

			got, err := json.Marshal(GetConstraints(field))
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}
