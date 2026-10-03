package bufcheck

import (
	"context"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type tightening struct {
	field   protoreflect.FieldDescriptor
	against protoreflect.FieldDescriptor
	rule    string
}

func checkValidationNoTighten(
	_ context.Context,
	responseWriter check.ResponseWriter,
	_ check.Request,
	message protoreflect.MessageDescriptor,
	against protoreflect.MessageDescriptor,
) error {
	for _, found := range findTightenings(message, against) {
		if found.against == nil {
			responseWriter.AddAnnotation(
				check.WithDescriptor(found.field),
				check.WithMessagef(
					"%q is new on %s and %s. An older client never sends it, so a newer server refuses every request that client makes. Leave a new field optional.",
					found.field.Name(),
					message.Name(),
					found.rule,
				),
			)

			continue
		}

		responseWriter.AddAnnotation(
			check.WithDescriptor(found.field),
			check.WithAgainstDescriptor(found.against),
			check.WithMessagef(
				"%q on %s now %s. An older client still sends what this refuses, so validation on an existing field may only loosen.",
				found.field.Name(),
				message.Name(),
				found.rule,
			),
		)
	}

	return nil
}

func findTightenings(message, against protoreflect.MessageDescriptor) []tightening {
	var found []tightening

	fields := message.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)

		previous := against.Fields().ByNumber(field.Number())
		if previous == nil {
			if refusesAbsent(field) {
				found = append(found, tightening{field: field, rule: "refuses it absent"})
			}

			continue
		}

		if rule := tightenedRule(fieldRules(field), fieldRules(previous)); rule != "" {
			found = append(found, tightening{field: field, against: previous, rule: rule})
		}
	}

	return found
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

func refusesAbsent(field protoreflect.FieldDescriptor) bool {
	rules := fieldRules(field)
	if rules == nil {
		return false
	}

	if rules.GetRequired() {
		return true
	}

	return refusesEmpty(field) && !field.HasPresence() && rules.GetIgnore() == validate.Ignore_IGNORE_UNSPECIFIED
}
