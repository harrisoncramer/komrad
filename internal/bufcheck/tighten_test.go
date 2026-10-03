package bufcheck

import (
	"testing"

	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestFindTightenings covers validation an older client's request would newly fail.
func TestFindTightenings(t *testing.T) {
	t.Parallel()

	minLen := stringRules(&validate.StringRules{MinLen: xptr.Ptr(uint64(1))})
	definedOnly := &validate.FieldRules{Type: &validate.FieldRules_Enum{Enum: &validate.EnumRules{DefinedOnly: xptr.Ptr(true)}}}
	ignoreZero := &validate.FieldRules{
		Ignore: validate.Ignore_IGNORE_IF_ZERO_VALUE.Enum(),
		Type:   &validate.FieldRules_String_{String_: &validate.StringRules{MinLen: xptr.Ptr(uint64(1))}},
	}

	tests := []struct {
		name    string
		against []*descriptorpb.FieldDescriptorProto
		message []*descriptorpb.FieldDescriptorProto
		want    []string
	}{
		{
			name:    "an existing string that starts refusing empty is flagged",
			against: []*descriptorpb.FieldDescriptorProto{stringField(nil, false)},
			message: []*descriptorpb.FieldDescriptorProto{stringField(minLen, false)},
			want:    []string{"carries a rule it did not"},
		},
		{
			name:    "an existing enum that starts refusing undefined values is flagged",
			against: []*descriptorpb.FieldDescriptorProto{enumField(nil)},
			message: []*descriptorpb.FieldDescriptorProto{enumField(definedOnly)},
			want:    []string{"carries a rule it did not"},
		},
		{
			name:    "an existing field whose validation loosens is allowed",
			against: []*descriptorpb.FieldDescriptorProto{stringField(minLen, false), enumField(definedOnly)},
			message: []*descriptorpb.FieldDescriptorProto{stringField(nil, false), enumField(nil)},
		},
		{
			name:    "an existing string whose minimum length rises is flagged",
			against: []*descriptorpb.FieldDescriptorProto{stringField(minLen, false)},
			message: []*descriptorpb.FieldDescriptorProto{stringField(stringRules(&validate.StringRules{MinLen: xptr.Ptr(uint64(3))}), false)},
			want:    []string{"carries a rule it did not"},
		},
		{
			name:    "an existing string that gains a pattern is flagged",
			against: []*descriptorpb.FieldDescriptorProto{stringField(minLen, false)},
			message: []*descriptorpb.FieldDescriptorProto{stringField(stringRules(&validate.StringRules{MinLen: xptr.Ptr(uint64(1)), Pattern: xptr.Ptr("^a")}), false)},
			want:    []string{"carries a rule it did not"},
		},
		{
			name:    "an existing string that stops skipping its zero value is flagged",
			against: []*descriptorpb.FieldDescriptorProto{stringField(ignoreZero, false)},
			message: []*descriptorpb.FieldDescriptorProto{stringField(minLen, false)},
			want:    []string{"skips fewer values than it did"},
		},
		{
			name:    "an existing string that skips its zero value and becomes required is flagged",
			against: []*descriptorpb.FieldDescriptorProto{stringField(ignoreZero, false)},
			message: []*descriptorpb.FieldDescriptorProto{stringField(&validate.FieldRules{
				Required: xptr.Ptr(true),
				Ignore:   validate.Ignore_IGNORE_IF_ZERO_VALUE.Enum(),
				Type:     &validate.FieldRules_String_{String_: &validate.StringRules{MinLen: xptr.Ptr(uint64(1))}},
			}, false)},
			want: []string{"carries a rule it did not"},
		},
		{
			name:    "an existing string that starts skipping its zero value is allowed",
			against: []*descriptorpb.FieldDescriptorProto{stringField(minLen, false)},
			message: []*descriptorpb.FieldDescriptorProto{stringField(ignoreZero, false)},
		},
		{
			name:    "an existing string that drops one of its rules is allowed",
			against: []*descriptorpb.FieldDescriptorProto{stringField(stringRules(&validate.StringRules{MinLen: xptr.Ptr(uint64(1)), Pattern: xptr.Ptr("^a")}), false)},
			message: []*descriptorpb.FieldDescriptorProto{stringField(minLen, false)},
		},
		{
			name:    "an existing field whose rules are unchanged is allowed",
			against: []*descriptorpb.FieldDescriptorProto{stringField(minLen, false), enumField(definedOnly)},
			message: []*descriptorpb.FieldDescriptorProto{stringField(minLen, false), enumField(definedOnly)},
		},
		{
			name:    "a new required field is flagged",
			message: []*descriptorpb.FieldDescriptorProto{stringField(&validate.FieldRules{Required: xptr.Ptr(true)}, true)},
			want:    []string{"refuses it absent"},
		},
		{
			name:    "a new string without presence that refuses empty is flagged",
			message: []*descriptorpb.FieldDescriptorProto{stringField(minLen, false)},
			want:    []string{"refuses it absent"},
		},
		{
			name:    "a new optional string that refuses empty is allowed",
			message: []*descriptorpb.FieldDescriptorProto{stringField(minLen, true)},
		},
		{
			name:    "a new string that ignores its zero value is allowed",
			message: []*descriptorpb.FieldDescriptorProto{stringField(ignoreZero, false)},
		},
		{
			name:    "a new enum that refuses undefined values is allowed",
			message: []*descriptorpb.FieldDescriptorProto{enumField(definedOnly)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rules := xslice.Map(findTightenings(buildRequest(t, tt.message), buildRequest(t, tt.against)), func(found tightening) string {
				return found.rule
			})

			assert.ElementsMatch(t, tt.want, rules)
		})
	}
}

func stringField(rules *validate.FieldRules, optional bool) *descriptorpb.FieldDescriptorProto {
	field := &descriptorpb.FieldDescriptorProto{
		Name:     xptr.Ptr("name"),
		Number:   xptr.Ptr(int32(1)),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		JsonName: xptr.Ptr("name"),
		Options:  fieldOptions(rules),
	}

	if optional {
		field.Proto3Optional = xptr.Ptr(true)
	}

	return field
}

func enumField(rules *validate.FieldRules) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:     xptr.Ptr("colour"),
		Number:   xptr.Ptr(int32(2)),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
		TypeName: xptr.Ptr(".tighten.v1.Colour"),
		JsonName: xptr.Ptr("colour"),
		Options:  fieldOptions(rules),
	}
}

func fieldOptions(rules *validate.FieldRules) *descriptorpb.FieldOptions {
	options := &descriptorpb.FieldOptions{}
	if rules != nil {
		proto.SetExtension(options, validate.E_Field, rules)
	}

	return options
}

func buildRequest(t *testing.T, fields []*descriptorpb.FieldDescriptorProto) protoreflect.MessageDescriptor {
	t.Helper()

	message := &descriptorpb.DescriptorProto{Name: xptr.Ptr("TightenRequest")}

	for _, field := range fields {
		field = proto.CloneOf(field)
		if field.GetProto3Optional() {
			field.OneofIndex = xptr.Ptr(int32(len(message.OneofDecl)))
			message.OneofDecl = append(message.OneofDecl, &descriptorpb.OneofDescriptorProto{Name: xptr.Ptr("_" + field.GetName())})
		}

		message.Field = append(message.Field, field)
	}

	file := &descriptorpb.FileDescriptorProto{
		Name:    xptr.Ptr("tighten/v1/tighten.proto"),
		Package: xptr.Ptr("tighten.v1"),
		Syntax:  xptr.Ptr("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: xptr.Ptr("Colour"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: xptr.Ptr("COLOUR_UNSPECIFIED"), Number: xptr.Ptr(int32(0))},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{message},
	}

	built, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	require.NoError(t, err)

	return built.Messages().Get(0)
}

func stringRules(rules *validate.StringRules) *validate.FieldRules {
	return &validate.FieldRules{Type: &validate.FieldRules_String_{String_: rules}}
}
