// Package protoenum reads the JSON names and labels komrad's proto enum values carry.
package protoenum

import (
	"fmt"
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

// Parse is the value of T whose JSON name is name, failing when T defines none.
func Parse[T enum](name string) (T, error) {
	var zero T
	for _, value := range Values[T]() {
		if Name(value) == name {
			return value, nil
		}
	}

	return zero, fmt.Errorf("%s defines no value named %q", zero.Descriptor().FullName(), name)
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

// ParseAll is the value of each name, failing on the first name T does not define.
func ParseAll[T enum](names []string) ([]T, error) {
	values := make([]T, len(names))
	for i, name := range names {
		value, err := Parse[T](name)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}

	return values, nil
}

// Accepted is each declared value of T but its zero that the field's validation rules accept.
func Accepted[T enum](field protoreflect.FieldDescriptor) []T {
	return xslice.Map(AcceptedOf(field), func(value protoreflect.EnumValueDescriptor) T {
		return T(value.Number())
	})
}

// AcceptedOf is each declared value of the field's enum but its zero that the field's validation rules accept.
func AcceptedOf(field protoreflect.FieldDescriptor) []protoreflect.EnumValueDescriptor {
	return slices.DeleteFunc(ValuesOf(field.Enum()), func(value protoreflect.EnumValueDescriptor) bool {
		return !IsAccepted(field, value.Number())
	})
}

// IsAccepted reports whether the field's validation rules accept the enum number.
func IsAccepted(field protoreflect.FieldDescriptor, number protoreflect.EnumNumber) bool {
	return !slices.Contains(enumRules(field).GetNotIn(), int32(number))
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
