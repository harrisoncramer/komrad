package bufcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/bufplugin/check"
	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type tightening struct {
	descriptor protoreflect.Descriptor
	against    protoreflect.Descriptor
	rule       string
}

func checkValidationNoTighten(
	_ context.Context,
	responseWriter check.ResponseWriter,
	_ check.Request,
	message protoreflect.MessageDescriptor,
	against protoreflect.MessageDescriptor,
) error {
	tightenings, err := findTightenings(message, against)
	if err != nil {
		return err
	}

	for _, found := range tightenings {
		subject := fmt.Sprintf("%q on %s", found.descriptor.Name(), message.Name())
		if found.descriptor == message {
			subject = string(message.Name())
		}

		if found.against == nil {
			responseWriter.AddAnnotation(
				check.WithDescriptor(found.descriptor),
				check.WithMessagef(
					"%s is new and %s. An older client never sends it, so a newer server refuses every request that client makes. Leave a new field optional.",
					subject,
					found.rule,
				),
			)

			continue
		}

		responseWriter.AddAnnotation(
			check.WithDescriptor(found.descriptor),
			check.WithAgainstDescriptor(found.against),
			check.WithMessagef(
				"%s now %s. An older client still sends what this refuses, so validation may only loosen.",
				subject,
				found.rule,
			),
		)
	}

	return nil
}

func findTightenings(message, against protoreflect.MessageDescriptor) ([]tightening, error) {
	types, err := extensionTypes(message.ParentFile())
	if err != nil {
		return nil, err
	}

	againstTypes, err := extensionTypes(against.ParentFile())
	if err != nil {
		return nil, err
	}

	refused, err := refusedWhenEmpty(message, types)
	if err != nil {
		return nil, err
	}

	var found []tightening

	fields := message.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)

		previous := against.Fields().ByNumber(field.Number())
		if previous == nil {
			if refused[field.Name()] {
				found = append(found, tightening{descriptor: field, rule: "refuses it absent"})
			}

			continue
		}

		rules, err := resolvedFieldRules(field, types)
		if err != nil {
			return nil, err
		}

		previousRules, err := resolvedFieldRules(previous, againstTypes)
		if err != nil {
			return nil, err
		}

		if rule := tightenedRule(rules, previousRules); rule != "" {
			found = append(found, tightening{descriptor: field, against: previous, rule: rule})
		}
	}

	oneofs := message.Oneofs()
	for i := range oneofs.Len() {
		oneof := oneofs.Get(i)
		if oneof.IsSynthetic() {
			continue
		}

		previous := against.Oneofs().ByName(oneof.Name())
		if previous == nil {
			if refused[oneof.Name()] {
				found = append(found, tightening{descriptor: oneof, rule: "refuses it absent"})
			}

			continue
		}

		if !isCoveredBy(oneofRules(oneof).ProtoReflect(), oneofRules(previous).ProtoReflect()) {
			found = append(found, tightening{descriptor: oneof, against: previous, rule: "carries a rule it did not"})
		}
	}

	if !isCoveredBy(messageRules(message).ProtoReflect(), messageRules(against).ProtoReflect()) {
		found = append(found, tightening{descriptor: message, against: against, rule: "carries a rule it did not"})
	}

	return found, nil
}

// refusedWhenEmpty names the fields and oneofs whose validation refuses a message an older client
// sends without them.
func refusedWhenEmpty(message protoreflect.MessageDescriptor, types *protoregistry.Types) (map[protoreflect.Name]bool, error) {
	validator, err := protovalidate.New(protovalidate.WithExtensionTypeResolver(types))
	if err != nil {
		return nil, err
	}

	refused := map[protoreflect.Name]bool{}

	err = validator.Validate(dynamicpb.NewMessage(message))

	var validationErr *protovalidate.ValidationError
	if err != nil && !errors.As(err, &validationErr) {
		return nil, err
	}

	if validationErr == nil {
		return refused, nil
	}

	for _, violation := range validationErr.Violations {
		if elements := violation.Proto.GetField().GetElements(); len(elements) > 0 {
			refused[protoreflect.Name(elements[0].GetFieldName())] = true
		}
	}

	return refused, nil
}

// tightenedRule names how rules refuse something previous accepted, or is empty when they do not.
func tightenedRule(rules, previous *validate.FieldRules) string {
	if rules == nil || rules.GetIgnore() == validate.Ignore_IGNORE_ALWAYS {
		return ""
	}

	if previous == nil {
		previous = &validate.FieldRules{}
	}

	if ignoreReach(rules.GetIgnore()) < ignoreReach(previous.GetIgnore()) {
		return "skips fewer values than it did"
	}

	rules, previous = proto.CloneOf(rules), proto.CloneOf(previous)
	rules.ClearIgnore()
	previous.ClearIgnore()

	if !isCoveredBy(rules.ProtoReflect(), previous.ProtoReflect()) {
		return "carries a rule it did not"
	}

	return ""
}

// ignoreReach orders the ignore settings by how many values each skips validating.
func ignoreReach(ignore validate.Ignore) int {
	switch ignore {
	case validate.Ignore_IGNORE_ALWAYS:
		return 2
	case validate.Ignore_IGNORE_IF_ZERO_VALUE:
		return 1
	case validate.Ignore_IGNORE_UNSPECIFIED:
	}

	return 0
}

// isCoveredBy reports whether every rule set on rules is set to the same value on previous.
func isCoveredBy(rules, previous protoreflect.Message) bool {
	if len(rules.GetUnknown()) > 0 && !bytes.Equal(rules.GetUnknown(), previous.GetUnknown()) {
		return false
	}

	covered := true

	rules.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case !previous.Has(field):
			covered = false
		case field.Message() != nil && !field.IsList() && !field.IsMap():
			covered = isCoveredBy(value.Message(), previous.Get(field).Message())
		default:
			covered = value.Equal(previous.Get(field))
		}

		return covered
	})

	return covered
}

// resolvedFieldRules reads a field's validation afresh with types, so the repo's own rules are
// known fields on both sides of a comparison rather than unknown bytes on one.
func resolvedFieldRules(field protoreflect.FieldDescriptor, types *protoregistry.Types) (*validate.FieldRules, error) {
	rules := fieldRules(field)
	if rules == nil {
		return &validate.FieldRules{}, nil
	}

	data, err := proto.Marshal(rules)
	if err != nil {
		return nil, err
	}

	resolved := &validate.FieldRules{}
	if err := (proto.UnmarshalOptions{Resolver: types}).Unmarshal(data, resolved); err != nil {
		return nil, err
	}

	return resolved, nil
}

// oneofRules reads the validation a oneof carries, or empty rules when it carries none.
func oneofRules(oneof protoreflect.OneofDescriptor) *validate.OneofRules {
	opts, ok := oneof.Options().(*descriptorpb.OneofOptions)
	if !ok || !proto.HasExtension(opts, validate.E_Oneof) {
		return &validate.OneofRules{}
	}

	rules, ok := proto.GetExtension(opts, validate.E_Oneof).(*validate.OneofRules)
	if !ok {
		return &validate.OneofRules{}
	}

	return rules
}

// messageRules reads the validation a message carries, or empty rules when it carries none.
func messageRules(message protoreflect.MessageDescriptor) *validate.MessageRules {
	opts, ok := message.Options().(*descriptorpb.MessageOptions)
	if !ok || !proto.HasExtension(opts, validate.E_Message) {
		return &validate.MessageRules{}
	}

	rules, ok := proto.GetExtension(opts, validate.E_Message).(*validate.MessageRules)
	if !ok {
		return &validate.MessageRules{}
	}

	return rules
}

// extensionTypes gathers the extensions a file and its imports declare, so validation can read
// rules the repo defines itself.
func extensionTypes(file protoreflect.FileDescriptor) (*protoregistry.Types, error) {
	types := &protoregistry.Types{}
	seen := map[string]bool{}

	var visit func(file protoreflect.FileDescriptor) error
	visit = func(file protoreflect.FileDescriptor) error {
		if seen[file.Path()] {
			return nil
		}

		seen[file.Path()] = true

		if err := registerExtensions(types, file.Extensions(), file.Messages()); err != nil {
			return err
		}

		imports := file.Imports()
		for i := range imports.Len() {
			if err := visit(imports.Get(i).FileDescriptor); err != nil {
				return err
			}
		}

		return nil
	}

	if err := visit(file); err != nil {
		return nil, err
	}

	return types, nil
}

// registerExtensions adds extensions, and those nested in messages, to types.
func registerExtensions(types *protoregistry.Types, extensions protoreflect.ExtensionDescriptors, messages protoreflect.MessageDescriptors) error {
	for i := range extensions.Len() {
		if err := types.RegisterExtension(dynamicpb.NewExtensionType(extensions.Get(i))); err != nil {
			return err
		}
	}

	for i := range messages.Len() {
		message := messages.Get(i)
		if err := registerExtensions(types, message.Extensions(), message.Messages()); err != nil {
			return err
		}
	}

	return nil
}
