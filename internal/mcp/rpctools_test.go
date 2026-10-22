package mcp

import (
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"
	annotationv1 "komrad/proto/gen/annotation/v1"
	configv1 "komrad/proto/gen/config/v1"
	hookv1 "komrad/proto/gen/hook/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"komrad/internal/rpcapi"
	"komrad/lib/xptr"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryMethodDeclaresOneSurface fails when an RPC declares neither a tool annotation nor a
// panel-only one.
func TestEveryMethodDeclaresOneSurface(t *testing.T) {
	tests := []struct {
		name string
		file protoreflect.FileDescriptor
	}{
		{name: "agent service", file: agentv1.File_agent_v1_agent_proto},
		{name: "workflow service", file: workflowv1.File_workflow_v1_workflow_proto},
		{name: "config service", file: configv1.File_config_v1_config_proto},
		{name: "hook service", file: hookv1.File_hook_v1_hook_proto},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := tt.file.Services().Get(0)
			require.Positive(t, service.Methods().Len())

			for i := range service.Methods().Len() {
				method := service.Methods().Get(i)
				surfaces := rpcapi.GetSurfaces(method)
				require.NotNil(t, surfaces, "method %s says nothing about who reaches it", method.Name())

				if !surfaces.GetCli() && !surfaces.GetMcp() {
					assert.NotEmpty(t, surfaces.GetReason(), "method %s is kept off both surfaces for no stated reason", method.Name())

					continue
				}

				assert.True(t, surfaces.GetCli(), "method %s is a tool without a command", method.Name())
				assert.NotEmpty(t, surfaces.GetName(), "method %s is on a surface with no name", method.Name())
				assert.NotEmpty(t, surfaces.GetDescription(), "tool %s needs a description", surfaces.GetName())
			}
		})
	}
}

// TestToolHints checks the hints a client uses to decide whether to confirm a call, where a
// read that looks destructive and a delete that looks safe both matter.
func TestToolHints(t *testing.T) {
	tests := []struct {
		method          string
		readOnly        bool
		idempotent      bool
		wantDestructive *bool
	}{
		{
			method:     "ListAgents",
			readOnly:   true,
			idempotent: true,
		},
		{
			method:     "GetAgent",
			readOnly:   true,
			idempotent: true,
		},
		{
			method:     "SearchTranscripts",
			readOnly:   true,
			idempotent: true,
		},
		{
			method:     "GetTranscript",
			readOnly:   true,
			idempotent: true,
		},
		{
			method:     "GetLastTurns",
			readOnly:   true,
			idempotent: true,
		},
		{
			method:          "ForwardLastTurns",
			readOnly:        false,
			idempotent:      false,
			wantDestructive: xptr.Ptr(false),
		},
		{
			method:          "CreateAgent",
			readOnly:        false,
			idempotent:      false,
			wantDestructive: xptr.Ptr(false),
		},
		{
			method:          "UpdateAgent",
			readOnly:        false,
			idempotent:      true,
			wantDestructive: xptr.Ptr(false),
		},
		{
			method:          "DeleteMail",
			readOnly:        false,
			idempotent:      true,
			wantDestructive: xptr.Ptr(true),
		},
		{
			method:     "ListReviewThreads",
			readOnly:   true,
			idempotent: true,
		},
		{
			method:     "ListReviewSnapshots",
			readOnly:   true,
			idempotent: true,
		},
	}

	service := agentv1.File_agent_v1_agent_proto.Services().Get(0)
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			method := service.Methods().ByName(protoreflect.Name(tt.method))
			require.NotNil(t, method)

			hints := toolHints(method, rpcapi.GetSurfaces(method))
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

// TestToolDescription confirms a tool's guidance follows its description.
func TestToolDescription(t *testing.T) {
	tests := []struct {
		name     string
		surfaces *annotationv1.Surfaces
		want     string
	}{
		{
			name:     "description alone",
			surfaces: &annotationv1.Surfaces{Description: "Archive a task."},
			want:     "Archive a task.",
		},
		{
			name:     "guidance after description",
			surfaces: &annotationv1.Surfaces{Description: "Archive a task.", McpGuidance: "Archive one only when asked."},
			want:     "Archive a task.\n\nArchive one only when asked.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toolDescription(tt.surfaces))
		})
	}
}

// TestInputSchema confirms each field carries its constraints and only what the server rejects
// empty is required.
func TestInputSchema(t *testing.T) {
	agentService := agentv1.File_agent_v1_agent_proto.Services().Get(0)

	tests := []struct {
		service      protoreflect.ServiceDescriptor
		method       string
		wantFields   map[string]map[string]any
		wantNested   map[string]map[string]map[string]any
		wantRequired []string
	}{
		{
			service: agentService,
			method:  "CreateAgent",
			wantFields: map[string]map[string]any{
				"prompt":      {"type": "string", "minLength": float64(1), "maxLength": float64(50000)},
				"workflow_id": {"type": "string", "format": "uuid"},
				"title": {
					"type": "string", "maxLength": float64(200), "description": "must not contain control characters",
				},
			},
			wantRequired: []string{"prompt", "workflow_id"},
		},
		{
			service:    agentService,
			method:     "ListAgents",
			wantFields: map[string]map[string]any{"pagination": nil},
			wantNested: map[string]map[string]map[string]any{
				"pagination": {
					"limit":  {"type": "integer", "minimum": float64(0)},
					"cursor": {"type": "string", "maxLength": float64(2048)},
				},
			},
		},
		{
			service:      agentService,
			method:       "GetAgent",
			wantFields:   map[string]map[string]any{"id": {"type": "string", "format": "uuid"}},
			wantRequired: []string{"id"},
		},
		{
			service: agentService,
			method:  "ArchiveAgents",
			wantFields: map[string]map[string]any{
				"ids": {
					"type":        "array",
					"items":       map[string]any{"type": "string", "format": "uuid"},
					"minItems":    float64(1),
					"uniqueItems": true,
				},
			},
			wantRequired: []string{"ids"},
		},
		{
			service: taskv1.File_task_v1_task_proto.Services().Get(0),
			method:  "SetTaskNotes",
			wantFields: map[string]map[string]any{
				"notes_updated_at": {"type": "string", "format": "date-time"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			method := tt.service.Methods().ByName(protoreflect.Name(tt.method))
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
				"only the fields the server rejects empty are required")
		})
	}
}

// TestDecodeFailure covers the text an agent receives when a call fails.
func TestDecodeFailure(t *testing.T) {
	type decodeFailureCase struct {
		name    string
		payload string
		want    string
	}

	tests := []decodeFailureCase{
		{
			name:    "a body that is not json comes back as it was",
			payload: "upstream connect error\n",
			want:    "upstream connect error",
		},
		{
			name:    "json without a code comes back as it was",
			payload: `{"message":"no code"}`,
			want:    `{"message":"no code"}`,
		},
		{
			name:    "an envelope without a message names its code",
			payload: `{"code":"internal"}`,
			want:    "internal",
		},
		{
			name:    "an envelope names its code and message",
			payload: `{"code":"not_found","message":"no agent named \"nobody\""}`,
			want:    `not_found: no agent named "nobody"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.EqualError(t, decodeFailure([]byte(tt.payload)), tt.want)
		})
	}
}
