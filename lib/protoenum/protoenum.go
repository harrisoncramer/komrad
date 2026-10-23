// Package protoenum reads the JSON names and labels komrad's proto enum values carry.
package protoenum

import (
	"slices"
	"strconv"

	"komrad/lib/xslice"
	annotationv1 "komrad/proto/gen/annotation/v1"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/jsonenumvalueoptionspb"
)

// enum is a generated proto enum type.
type enum interface {
	~int32
	protoreflect.Enum
}

// Name is the value's JSON name, or its number for a value the enum does not define.
func Name(value protoreflect.Enum) string {
	defined := value.Descriptor().Values().ByNumber(value.Number())
	if defined == nil {
		return strconv.Itoa(int(value.Number()))
	}

	return NameOf(defined)
}

// NameOf is the JSON name of an enum value, its constant name unless the value sets another.
func NameOf(value protoreflect.EnumValueDescriptor) string {
	options, ok := proto.GetExtension(value.Options(), jsonenumvalueoptionspb.E_Json).(*jsonenumvalueoptionspb.JsonEnumValueOptions)
	if ok && options.GetString() != "" {
		return options.GetString()
	}

	return string(value.Name())
}

// Parse is the value of T whose JSON name is name, and false when T defines none.
func Parse[T enum](name string) (T, bool) {
	for _, value := range Values[T]() {
		if Name(value) == name {
			return value, true
		}
	}

	var zero T

	return zero, false
}

// Values is every value of T except its zero, in the order the enum declares them.
func Values[T enum]() []T {
	var zero T

	return xslice.Map(ValuesOf(zero.Descriptor()), func(value protoreflect.EnumValueDescriptor) T {
		return T(value.Number())
	})
}

// ValuesOf is every value of the enum except its zero, in the order the enum declares them.
func ValuesOf(enum protoreflect.EnumDescriptor) []protoreflect.EnumValueDescriptor {
	declared := enum.Values()
	values := make([]protoreflect.EnumValueDescriptor, 0, declared.Len())
	for i := range declared.Len() {
		if value := declared.Get(i); value.Number() != 0 {
			values = append(values, value)
		}
	}

	return values
}

// Label is the title and description an enum value carries, empty when it carries none.
func Label(value protoreflect.Enum) *annotationv1.Label {
	defined := value.Descriptor().Values().ByNumber(value.Number())
	if defined == nil {
		return &annotationv1.Label{}
	}

	return LabelOf(defined)
}

// LabelOf is the title and description an enum value carries, empty when it carries none.
func LabelOf(value protoreflect.EnumValueDescriptor) *annotationv1.Label {
	label, ok := proto.GetExtension(value.Options(), annotationv1.E_Label).(*annotationv1.Label)
	if !ok || label == nil {
		return &annotationv1.Label{}
	}

	return label
}

// Names is the JSON name of each value.
func Names[T enum](values []T) []string {
	names := make([]string, len(values))
	for i, value := range values {
		names[i] = Name(value)
	}

	return names
}

// ParseKnown is the value of each name T defines, skipping the names it does not.
func ParseKnown[T enum](names []string) []T {
	values := make([]T, 0, len(names))
	for _, name := range names {
		if value, ok := Parse[T](name); ok {
			values = append(values, value)
		}
	}

	return values
}

// Accepted is each declared value of T but its zero that the field's validation rules accept.
func Accepted[T enum](field protoreflect.FieldDescriptor) []T {
	accepted := AcceptedValues(field)
	values := make([]T, len(accepted))
	for i, value := range accepted {
		values[i] = T(value.Number())
	}

	return values
}

// AcceptedValues is each declared value of the field's enum but its zero that the field's validation rules accept.
func AcceptedValues(field protoreflect.FieldDescriptor) []protoreflect.EnumValueDescriptor {
	rules := enumRules(field)

	return slices.DeleteFunc(ValuesOf(field.Enum()), func(value protoreflect.EnumValueDescriptor) bool {
		number := int32(value.Number())

		return slices.Contains(rules.GetNotIn(), number) ||
			len(rules.GetIn()) > 0 && !slices.Contains(rules.GetIn(), number) ||
			rules.HasConst() && rules.GetConst() != number
	})
}

func enumRules(field protoreflect.FieldDescriptor) *validate.EnumRules {
	rules, ok := proto.GetExtension(field.Options(), validate.E_Field).(*validate.FieldRules)
	if !ok {
		return nil
	}
	if field.IsList() {
		return rules.GetRepeated().GetItems().GetEnum()
	}

	return rules.GetEnum()
}
