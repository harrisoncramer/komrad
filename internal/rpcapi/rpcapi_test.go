package rpcapi

import (
	"testing"
	"time"

	agentv1 "komrad/proto/gen/agent/v1"
	annotationv1 "komrad/proto/gen/annotation/v1"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestUnspecifiedIsServedEverything checks that a caller komrad cannot place loses nothing.
func TestUnspecifiedIsServedEverything(t *testing.T) {
	every := map[string]bool{}
	served := map[string]bool{}
	for exposed := range GetMethods() {
		if !exposed.Surfaces.GetMcp() {
			continue
		}

		every[exposed.Surfaces.GetName()] = true
		if ServesRole(exposed.Surfaces, annotationv1.Role_ROLE_UNSPECIFIED) {
			served[exposed.Surfaces.GetName()] = true
		}
	}

	assert.Equal(t, every, served)
}

// TestIsRequired checks a field in a oneof is never demanded on its own.
func TestIsRequired(t *testing.T) {
	tests := []struct {
		message protoreflect.MessageDescriptor
		field   string
		want    bool
	}{
		{message: (&agentv1.WriteDocumentRequest{}).ProtoReflect().Descriptor(), field: "name", want: true},
		{message: (&agentv1.WriteDocumentRequest{}).ProtoReflect().Descriptor(), field: "id", want: false},
		{message: (&agentv1.WriteDocumentRequest{}).ProtoReflect().Descriptor(), field: "body", want: false},
		{message: (&agentv1.WriteDocumentRequest{}).ProtoReflect().Descriptor(), field: "path", want: false},
		{message: (&agentv1.GetDocumentRequest{}).ProtoReflect().Descriptor(), field: "id", want: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.message.Name())+"."+tt.field, func(t *testing.T) {
			field := tt.message.Fields().ByName(protoreflect.Name(tt.field))
			require.NotNil(t, field)
			assert.Equal(t, tt.want, IsRequired(field))
		})
	}
}

// TestIsUUID checks a field reads as a UUID whether the rule sits on the field or on a list's items.
func TestIsUUID(t *testing.T) {
	tests := []struct {
		message protoreflect.MessageDescriptor
		field   string
		want    bool
	}{
		{message: (&agentv1.SendMailRequest{}).ProtoReflect().Descriptor(), field: "to_agent_id", want: true},
		{message: (&agentv1.SendMailRequest{}).ProtoReflect().Descriptor(), field: "reply_to", want: true},
		{message: (&agentv1.SendMailRequest{}).ProtoReflect().Descriptor(), field: "to_agent_name", want: false},
		{message: (&agentv1.ArchiveAgentsRequest{}).ProtoReflect().Descriptor(), field: "ids", want: true},
		{message: (&agentv1.Agent{}).ProtoReflect().Descriptor(), field: "id", want: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.message.Name())+"."+tt.field, func(t *testing.T) {
			field := tt.message.Fields().ByName(protoreflect.Name(tt.field))
			require.NotNil(t, field)
			assert.Equal(t, tt.want, IsUUID(field))
		})
	}
}

// TestIsOneofRequired checks only a oneof carrying the required rule must be set.
func TestIsOneofRequired(t *testing.T) {
	tests := []struct {
		message protoreflect.MessageDescriptor
		oneof   string
		want    bool
	}{
		{message: (&agentv1.SendMailRequest{}).ProtoReflect().Descriptor(), oneof: "to", want: true},
		{message: (&agentv1.CreateAgentRequest{}).ProtoReflect().Descriptor(), oneof: "task", want: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.message.Name())+"."+tt.oneof, func(t *testing.T) {
			oneof := tt.message.Oneofs().ByName(protoreflect.Name(tt.oneof))
			require.NotNil(t, oneof)
			assert.Equal(t, tt.want, IsOneofRequired(oneof))
		})
	}
}

// TestIsTimestamp checks only a google.protobuf.Timestamp counts, not any message.
func TestIsTimestamp(t *testing.T) {
	tests := []struct {
		message protoreflect.MessageDescriptor
		field   string
		want    bool
	}{
		{message: (&agentv1.ListAgentsRequest{}).ProtoReflect().Descriptor(), field: "pagination", want: false},
		{message: (&taskv1.SetTaskNotesRequest{}).ProtoReflect().Descriptor(), field: "notes", want: false},
		{message: (&taskv1.SetTaskNotesRequest{}).ProtoReflect().Descriptor(), field: "notes_updated_at", want: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.message.Name())+"."+tt.field, func(t *testing.T) {
			field := tt.message.Fields().ByName(protoreflect.Name(tt.field))
			require.NotNil(t, field)
			assert.Equal(t, tt.want, IsTimestamp(field))
		})
	}
}

// TestGetCallTimeout covers a call that waits on hooks carrying no deadline of its own, the server bounding it by the hooks.timeout setting.
func TestGetCallTimeout(t *testing.T) {
	tests := []struct {
		name string
		want time.Duration
	}{
		{name: "archive_agents", want: 0},
		{name: "resume_agent", want: 0},
		{name: "delete_workflow", want: 0},
		{name: "list_agents", want: 30 * time.Second},
	}

	byName := map[string]Method{}
	for exposed := range GetMethods() {
		byName[exposed.Surfaces.GetName()] = exposed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exposed, ok := byName[tt.name]
			require.True(t, ok)
			assert.Equal(t, tt.want, exposed.GetCallTimeout(30*time.Second))
		})
	}
}
