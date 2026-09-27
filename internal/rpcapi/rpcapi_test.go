package rpcapi

import (
	"testing"

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
