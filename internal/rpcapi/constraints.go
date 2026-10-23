package rpcapi

import (
	"komrad/lib/protoenum"
	"komrad/lib/xslice"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Constraints is what the server refuses of a field's value, named after JSON Schema's validation keywords.
type Constraints struct {
	MinLength        *uint64      `json:"minLength,omitempty"`
	MaxLength        *uint64      `json:"maxLength,omitempty"`
	MaxBytes         *uint64      `json:"maxBytes,omitempty"`
	Pattern          string       `json:"pattern,omitempty"`
	Format           string       `json:"format,omitempty"`
	Enum             []string     `json:"enum,omitempty"`
	Minimum          *int64       `json:"minimum,omitempty"`
	ExclusiveMinimum *int64       `json:"exclusiveMinimum,omitempty"`
	Maximum          *int64       `json:"maximum,omitempty"`
	ExclusiveMaximum *int64       `json:"exclusiveMaximum,omitempty"`
	MinItems         *uint64      `json:"minItems,omitempty"`
	MaxItems         *uint64      `json:"maxItems,omitempty"`
	UniqueItems      bool         `json:"uniqueItems,omitempty"`
	Items            *Constraints `json:"items,omitempty"`
	Rules            []string     `json:"rules,omitempty"`
}

// GetConstraints returns what the server refuses of the field's value beyond its type.
func GetConstraints(field protoreflect.FieldDescriptor) Constraints {
	rules := getRules(field)
	if !field.IsList() {
		return valueConstraints(field, rules)
	}

	repeated := rules.GetRepeated()
	list := Constraints{UniqueItems: repeated.GetUnique(), Rules: ruleMessages(rules)}
	if repeated.HasMinItems() {
		list.MinItems = new(repeated.GetMinItems())
	}
	if repeated.HasMaxItems() {
		list.MaxItems = new(repeated.GetMaxItems())
	}
	if repeated.HasItems() {
		list.Items = new(valueConstraints(field, repeated.GetItems()))
	}

	return list
}

func valueConstraints(field protoreflect.FieldDescriptor, rules *validate.FieldRules) Constraints {
	text := rules.GetString()
	constraints := Constraints{Pattern: text.GetPattern(), Enum: text.GetIn(), Rules: ruleMessages(rules)}
	if field.Kind() == protoreflect.EnumKind {
		constraints.Enum = narrowedEnum(field)
	}
	if text.HasMinLen() {
		constraints.MinLength = new(text.GetMinLen())
	}
	if text.HasMaxLen() {
		constraints.MaxLength = new(text.GetMaxLen())
	}
	if text.HasMaxBytes() {
		constraints.MaxBytes = new(text.GetMaxBytes())
	}
	if text.GetUuid() {
		constraints.Format = "uuid"
	}

	if set := ruleSet(rules); set != nil {
		constraints.Minimum = bound(set, "gte")
		constraints.ExclusiveMinimum = bound(set, "gt")
		constraints.Maximum = bound(set, "lte")
		constraints.ExclusiveMaximum = bound(set, "lt")
	}

	return constraints
}

// narrowedEnum returns the names of the enum values the field accepts, nil when it accepts every one.
func narrowedEnum(field protoreflect.FieldDescriptor) []string {
	if names := EnumNames(field); len(names) < len(protoenum.ValuesOf(field.Enum())) {
		return names
	}

	return nil
}

// ruleSet returns the rules for the field's kind, such as its string or int32 rules, nil when it carries none.
func ruleSet(rules *validate.FieldRules) protoreflect.Message {
	if rules == nil {
		return nil
	}

	message := rules.ProtoReflect()
	set := message.WhichOneof(message.Descriptor().Oneofs().ByName("type"))
	if set == nil || set.Message() == nil {
		return nil
	}

	return message.Get(set).Message()
}

// bound returns an integer rule set's named bound, nil when the set has none.
func bound(set protoreflect.Message, name protoreflect.Name) *int64 {
	field := set.Descriptor().Fields().ByName(name)
	if field == nil || !set.Has(field) {
		return nil
	}

	switch value := set.Get(field).Interface().(type) {
	case int32:
		return new(int64(value))
	case int64:
		return new(value)
	case uint32:
		return new(int64(value))
	case uint64:
		return new(int64(value))
	default:
		return nil
	}
}

// ruleMessages returns the messages of the CEL rules on the field and of those predefined on its rule set.
func ruleMessages(rules *validate.FieldRules) []string {
	messages := celMessages(rules.GetCel())

	set := ruleSet(rules)
	if set == nil {
		return messages
	}

	set.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if !field.IsExtension() || field.Kind() != protoreflect.BoolKind || !value.Bool() {
			return true
		}

		opts, ok := field.Options().(*descriptorpb.FieldOptions)
		if !ok {
			return true
		}

		if predefined, ok := proto.GetExtension(opts, validate.E_Predefined).(*validate.PredefinedRules); ok {
			messages = append(messages, celMessages(predefined.GetCel())...)
		}

		return true
	})

	return messages
}

func celMessages(rules []*validate.Rule) []string {
	return xslice.Map(rules, (*validate.Rule).GetMessage)
}
