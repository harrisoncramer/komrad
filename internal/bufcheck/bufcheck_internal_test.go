package bufcheck

import (
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	"weave/lib/xptr"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestRefusesEmpty reads the rules off weave's own descriptors.
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

// TestIsDefinedOnly reads the enum rules off weave's own descriptors.
func TestIsDefinedOnly(t *testing.T) {
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

			assert.Equal(t, tt.want, isDefinedOnly(field))
		})
	}
}

// TestIsDefinedOnlyOnAList builds the descriptor rather than compiling one.
func TestIsDefinedOnlyOnAList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rules *validate.FieldRules
		want  bool
	}{
		{
			name: "the list spelling",
			rules: &validate.FieldRules{
				Type: &validate.FieldRules_Repeated{
					Repeated: &validate.RepeatedRules{
						Items: &validate.FieldRules{
							Type: &validate.FieldRules_Enum{
								Enum: &validate.EnumRules{DefinedOnly: xptr.Ptr(true)},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "the scalar spelling, which protovalidate refuses on a list",
			rules: &validate.FieldRules{
				Type: &validate.FieldRules_Enum{
					Enum: &validate.EnumRules{DefinedOnly: xptr.Ptr(true)},
				},
			},
			want: false,
		},
		{
			name:  "no rules at all",
			rules: nil,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			field := repeatedEnumField(t, tt.rules)
			assert.Equal(t, tt.want, isDefinedOnly(field))
		})
	}
}

func repeatedEnumField(t *testing.T, rules *validate.FieldRules) protoreflect.FieldDescriptor {
	t.Helper()

	options := &descriptorpb.FieldOptions{}
	if rules != nil {
		proto.SetExtension(options, validate.E_Field, rules)
	}

	file := &descriptorpb.FileDescriptorProto{
		Name:    xptr.Ptr("list/v1/list.proto"),
		Package: xptr.Ptr("list.v1"),
		Syntax:  xptr.Ptr("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: xptr.Ptr("Colour"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: xptr.Ptr("COLOUR_UNSPECIFIED"), Number: xptr.Ptr(int32(0))},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: xptr.Ptr("ListRequest"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     xptr.Ptr("colours"),
				Number:   xptr.Ptr(int32(1)),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
				TypeName: xptr.Ptr(".list.v1.Colour"),
				JsonName: xptr.Ptr("colours"),
				Options:  options,
			}},
		}},
	}

	built, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	require.NoError(t, err)

	field := built.Messages().Get(0).Fields().Get(0)
	require.True(t, field.IsList())

	return field
}

// TestIsDefinedOnlyOnAMap builds the descriptor rather than compiling one.
func TestIsDefinedOnlyOnAMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rules *validate.FieldRules
		want  bool
	}{
		{
			name: "the map spelling",
			rules: &validate.FieldRules{
				Type: &validate.FieldRules_Map{
					Map: &validate.MapRules{
						Values: &validate.FieldRules{
							Type: &validate.FieldRules_Enum{
								Enum: &validate.EnumRules{DefinedOnly: xptr.Ptr(true)},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "the scalar spelling, which protovalidate refuses on a map",
			rules: &validate.FieldRules{
				Type: &validate.FieldRules_Enum{
					Enum: &validate.EnumRules{DefinedOnly: xptr.Ptr(true)},
				},
			},
			want: false,
		},
		{
			name: "the list spelling, which is the wrong cardinality too",
			rules: &validate.FieldRules{
				Type: &validate.FieldRules_Repeated{
					Repeated: &validate.RepeatedRules{
						Items: &validate.FieldRules{
							Type: &validate.FieldRules_Enum{
								Enum: &validate.EnumRules{DefinedOnly: xptr.Ptr(true)},
							},
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			field := enumMapField(t, tt.rules)
			assert.Equal(t, tt.want, isDefinedOnly(field))
		})
	}
}

func enumMapField(t *testing.T, rules *validate.FieldRules) protoreflect.FieldDescriptor {
	t.Helper()

	options := &descriptorpb.FieldOptions{}
	proto.SetExtension(options, validate.E_Field, rules)

	file := &descriptorpb.FileDescriptorProto{
		Name:    xptr.Ptr("keyed/v1/keyed.proto"),
		Package: xptr.Ptr("keyed.v1"),
		Syntax:  xptr.Ptr("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: xptr.Ptr("Colour"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: xptr.Ptr("COLOUR_UNSPECIFIED"), Number: xptr.Ptr(int32(0))},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: xptr.Ptr("KeyedRequest"),
			NestedType: []*descriptorpb.DescriptorProto{{
				Name:    xptr.Ptr("PaletteEntry"),
				Options: &descriptorpb.MessageOptions{MapEntry: xptr.Ptr(true)},
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     xptr.Ptr("key"),
						Number:   xptr.Ptr(int32(1)),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: xptr.Ptr("key"),
					},
					{
						Name:     xptr.Ptr("value"),
						Number:   xptr.Ptr(int32(2)),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
						TypeName: xptr.Ptr(".keyed.v1.Colour"),
						JsonName: xptr.Ptr("value"),
					},
				},
			}},
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     xptr.Ptr("palette"),
				Number:   xptr.Ptr(int32(1)),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: xptr.Ptr(".keyed.v1.KeyedRequest.PaletteEntry"),
				JsonName: xptr.Ptr("palette"),
				Options:  options,
			}},
		}},
	}

	built, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	require.NoError(t, err)

	field := built.Messages().Get(0).Fields().Get(0)
	require.True(t, field.IsMap())

	return field
}
