package bufcheck

import (
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestRefusesEmpty reads the rules off weave's own descriptors, which is the shape the fixture
// cannot cover without a copy of validate.proto beside it.
func TestRefusesEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    protoreflect.FileDescriptor
		message protoreflect.Name
		field   protoreflect.Name
		want    bool
	}{
		{
			name:    "a string with a minimum length",
			file:    agentv1.File_agent_v1_agent_proto,
			message: "GetAgentRequest",
			field:   "id",
			want:    true,
		},
		{
			name:    "a repeated field with a minimum count",
			file:    agentv1.File_agent_v1_agent_proto,
			message: "ArchiveAgentsRequest",
			field:   "ids",
			want:    true,
		},
		{
			name:    "an optional string with a minimum length",
			file:    agentv1.File_agent_v1_mail_proto,
			message: "ListMailRequest",
			field:   "id",
			want:    true,
		},
		{
			name:    "a field carrying no rules at all",
			file:    agentv1.File_agent_v1_agent_proto,
			message: "Agent",
			field:   "id",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			message := tt.file.Messages().ByName(tt.message)
			require.NotNil(t, message)

			field := message.Fields().ByName(tt.field)
			require.NotNil(t, field)

			assert.Equal(t, tt.want, refusesEmpty(field))
		})
	}
}

// TestDefinedOnly reads the enum rules off weave's own descriptors, the shape the fixture cannot
// cover without a copy of validate.proto beside it.
func TestDefinedOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    protoreflect.FileDescriptor
		message protoreflect.Name
		field   protoreflect.Name
		want    bool
	}{
		{
			name:    "an enum field declaring defined_only",
			file:    agentv1.File_agent_v1_mail_proto,
			message: "ListMailRequest",
			field:   "direction",
			want:    true,
		},
		{
			name:    "the view field this rule was written for",
			file:    agentv1.File_agent_v1_agent_proto,
			message: "ListAgentsRequest",
			field:   "view",
			want:    true,
		},
		{
			name:    "an entity enum, which validation never reaches",
			file:    agentv1.File_agent_v1_mail_proto,
			message: "Mail",
			field:   "kind",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			message := tt.file.Messages().ByName(tt.message)
			require.NotNil(t, message)

			field := message.Fields().ByName(tt.field)
			require.NotNil(t, field)

			assert.Equal(t, tt.want, definedOnly(field))
		})
	}
}
