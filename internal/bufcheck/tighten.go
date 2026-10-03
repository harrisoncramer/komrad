package bufcheck

import (
	"context"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/bufplugin/check"
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

		if refusesEmpty(field) && !refusesEmpty(previous) {
			found = append(found, tightening{field: field, against: previous, rule: "refuses it empty"})
		}

		if isDefinedOnly(field) && !isDefinedOnly(previous) {
			found = append(found, tightening{field: field, against: previous, rule: "refuses a value its enum does not define"})
		}
	}

	return found
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
