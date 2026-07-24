package mcp

import (
	"testing"

	sessionv1 "claude-agents/proto/gen/session/v1"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryMethodIsAnnotated confirms the tool surface keeps up with the
// service. A new RPC added without a tool annotation is invisible to a model,
// which is the safe default but is easy to do by accident, so this fails loudly
// enough to be a decision rather than an oversight.
func TestEveryMethodIsAnnotated(t *testing.T) {
	service := sessionv1.File_session_v1_session_proto.Services().Get(0)

	for i := range service.Methods().Len() {
		method := service.Methods().Get(i)

		annotation := toolAnnotation(method)
		require.NotNil(t, annotation, "method %s has no tool annotation", method.Name())
		assert.NotEmpty(t, annotation.GetDescription(), "tool %s needs a description", annotation.GetToolName())
	}
}

// TestToolHints checks the hints each method advertises. They are what a client
// uses to decide whether to confirm a call, so a read that looks destructive or
// a delete that looks safe both matter.
func TestToolHints(t *testing.T) {
	tests := []struct {
		method          string
		readOnly        bool
		idempotent      bool
		wantDestructive *bool
	}{
		{method: "ListSessions", readOnly: true, idempotent: true},
		{method: "GetSession", readOnly: true, idempotent: true},
		{method: "SearchTranscripts", readOnly: true, idempotent: true},
		{method: "GetTranscript", readOnly: true, idempotent: true},
		{method: "CreateSession", readOnly: false, idempotent: false, wantDestructive: boolPtr(false)},
		{method: "SetState", readOnly: false, idempotent: true, wantDestructive: boolPtr(false)},
		{method: "UpdateSession", readOnly: false, idempotent: true, wantDestructive: boolPtr(false)},
		{method: "DeleteSession", readOnly: false, idempotent: true, wantDestructive: boolPtr(true)},
	}

	service := sessionv1.File_session_v1_session_proto.Services().Get(0)
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			method := service.Methods().ByName(protoreflect.Name(tt.method))
			require.NotNil(t, method)

			hints := toolHints(method, toolAnnotation(method))
			assert.Equal(t, tt.readOnly, hints.ReadOnlyHint)
			assert.Equal(t, tt.idempotent, hints.IdempotentHint)

			if tt.wantDestructive == nil {
				assert.Nil(t, hints.DestructiveHint, "a read-only tool leaves the destructive hint unset")
				return
			}
			require.NotNil(t, hints.DestructiveHint)
			assert.Equal(t, *tt.wantDestructive, *hints.DestructiveHint)
		})
	}
}

// TestInputSchema confirms a request message is described in terms a client can
// satisfy: proto3 optional fields are optional, everything else is required.
func TestInputSchema(t *testing.T) {
	service := sessionv1.File_session_v1_session_proto.Services().Get(0)
	method := service.Methods().ByName("CreateSession")
	require.NotNil(t, method)

	schema, err := inputSchema(method.Input())
	require.NoError(t, err)

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, properties, "prompt")
	assert.Contains(t, properties, "workflowId")
	assert.Equal(t, map[string]any{"type": "string"}, properties["prompt"])

	required, ok := schema["required"].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"prompt", "dir"}, required,
		"only the fields the service rejects empty are required; a blank title or flags is fine")
}

// TestInputSchemaForEmptyRequest confirms a method taking no arguments still
// advertises an object, rather than a schema a client cannot match.
func TestInputSchemaForEmptyRequest(t *testing.T) {
	service := sessionv1.File_session_v1_session_proto.Services().Get(0)
	method := service.Methods().ByName("ListSessions")
	require.NotNil(t, method)

	schema, err := inputSchema(method.Input())
	require.NoError(t, err)

	assert.Equal(t, "object", schema["type"])
	assert.Empty(t, schema["properties"])
	assert.NotContains(t, schema, "required")
}

func boolPtr(b bool) *bool { return &b }
