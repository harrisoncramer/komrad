package mcp

import (
	"testing"

	actionv1 "weave/proto/gen/action/v1"
	agentv1 "weave/proto/gen/agent/v1"
	configv1 "weave/proto/gen/config/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"weave/lib/xptr"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryMethodIsAnnotated confirms the tool surface keeps up with the
// services. A new RPC added without a tool annotation is invisible to a model,
// which is the safe default but is easy to do by accident, so this fails loudly
// enough to be a decision rather than an oversight.
func TestEveryMethodIsAnnotated(t *testing.T) {
	tests := []struct {
		name string
		file protoreflect.FileDescriptor
	}{
		{name: "agent service", file: agentv1.File_agent_v1_agent_proto},
		{name: "workflow service", file: workflowv1.File_workflow_v1_workflow_proto},
		{name: "config service", file: configv1.File_config_v1_config_proto},
		{name: "action service", file: actionv1.File_action_v1_action_proto},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := tt.file.Services().Get(0)
			require.Positive(t, service.Methods().Len())

			for i := range service.Methods().Len() {
				method := service.Methods().Get(i)

				annotation := toolAnnotation(method)
				require.NotNil(t, annotation, "method %s has no tool annotation", method.Name())
				assert.NotEmpty(t, annotation.GetDescription(), "tool %s needs a description", annotation.GetToolName())
			}
		})
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
		{method: "ListAgents", readOnly: true, idempotent: true},
		{method: "GetAgent", readOnly: true, idempotent: true},
		{method: "SearchTranscripts", readOnly: true, idempotent: true},
		{method: "GetTranscript", readOnly: true, idempotent: true},
		{method: "CreateAgent", readOnly: false, idempotent: false, wantDestructive: xptr.Ptr(false)},
		{method: "SetMarks", readOnly: false, idempotent: true, wantDestructive: xptr.Ptr(false)},
		{method: "UpdateAgent", readOnly: false, idempotent: true, wantDestructive: xptr.Ptr(false)},
		{method: "DeleteAgent", readOnly: false, idempotent: true, wantDestructive: xptr.Ptr(true)},
	}

	service := agentv1.File_agent_v1_agent_proto.Services().Get(0)
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
// satisfy. What the service rejects empty is required, and everything else can
// be left out.
func TestInputSchema(t *testing.T) {
	tests := []struct {
		method string
		// wantFields is the exact schema expected for each named top-level field.
		wantFields map[string]map[string]any
		// wantNested is the same for the fields of a nested message field, keyed by
		// the field holding it.
		wantNested map[string]map[string]map[string]any
		// wantRequired is what the schema must demand. Nil means it must demand
		// nothing, so the tool can be called with no arguments.
		wantRequired []string
	}{
		{
			method:       "CreateAgent",
			wantFields:   map[string]map[string]any{"prompt": {"type": "string"}, "workflowId": {"type": "string"}},
			wantRequired: []string{"prompt", "workflowId"},
		},
		{
			method:     "ListAgents",
			wantFields: map[string]map[string]any{"pagination": nil},
			wantNested: map[string]map[string]map[string]any{
				"pagination": {"limit": {"type": "integer"}, "cursor": {"type": "string"}},
			},
		},
		{
			method:       "GetAgent",
			wantFields:   map[string]map[string]any{"id": {"type": "string"}},
			wantRequired: []string{"id"},
		},
	}

	service := agentv1.File_agent_v1_agent_proto.Services().Get(0)
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			method := service.Methods().ByName(protoreflect.Name(tt.method))
			require.NotNil(t, method)

			schema, err := inputSchema(method.Input())
			require.NoError(t, err)
			assert.Equal(t, "object", schema["type"])

			properties, ok := schema["properties"].(map[string]any)
			require.True(t, ok)

			for name, want := range tt.wantFields {
				require.Contains(t, properties, name)
				if want != nil {
					assert.Equal(t, want, properties[name])
				}
			}

			for name, wantFields := range tt.wantNested {
				nested, ok := properties[name].(map[string]any)
				require.True(t, ok, "%s is described rather than omitted", name)
				assert.Equal(t, "object", nested["type"])

				fields, ok := nested["properties"].(map[string]any)
				require.True(t, ok)
				for field, want := range wantFields {
					assert.Equal(t, want, fields[field])
				}
			}

			if tt.wantRequired == nil {
				assert.NotContains(t, schema, "required", "this tool can be called with no arguments at all")
				return
			}

			required, ok := schema["required"].([]string)
			require.True(t, ok)
			assert.ElementsMatch(t, tt.wantRequired, required,
				"only the fields the service rejects empty are required")
		})
	}
}
