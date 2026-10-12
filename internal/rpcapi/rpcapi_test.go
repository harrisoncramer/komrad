package rpcapi

import (
	"testing"
	"time"

	agentv1 "komrad/proto/gen/agent/v1"
	annotationv1 "komrad/proto/gen/annotation/v1"

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
		field string
		want  bool
	}{
		{field: "name", want: true},
		{field: "id", want: false},
		{field: "body", want: false},
		{field: "path", want: false},
	}

	message := (&agentv1.WriteDocumentRequest{}).ProtoReflect().Descriptor()
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			field := message.Fields().ByName(protoreflect.Name(tt.field))
			require.NotNil(t, field)
			assert.Equal(t, tt.want, IsRequired(field))
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
