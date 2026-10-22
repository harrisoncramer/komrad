package rpcapi

import (
	"encoding/json"
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"

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
