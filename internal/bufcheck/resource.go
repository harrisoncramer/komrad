package bufcheck

import (
	"context"

	annotationv1 "weave/proto/gen/annotation/v1"

	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// rowMessage is the message whose oneof carries one arm per resource.
const rowMessage = "watch.v1.Row"

// checkResource holds a resource option to its shape: a table, and for each projection a
// table and one way of finding the resource it belongs to.
func checkResource(
	_ context.Context,
	responseWriter check.ResponseWriter,
	_ check.Request,
	message protoreflect.MessageDescriptor,
) error {
	option := readResource(message)
	if option == nil {
		return nil
	}

	if option.GetTable() == "" {
		responseWriter.AddAnnotation(
			check.WithDescriptor(message),
			check.WithMessagef("%s carries (annotation.v1.resource) with no table, so nothing says which rows it is.", message.FullName()),
		)
	}

	for _, projection := range option.GetProjects() {
		byColumn := projection.GetColumn() != ""
		byLookup := projection.GetLookup() != "" || projection.GetVia() != ""

		switch {
		case projection.GetTable() == "":
			responseWriter.AddAnnotation(
				check.WithDescriptor(message),
				check.WithMessagef("%s projects a table with no name.", message.FullName()),
			)
		case byColumn == byLookup:
			responseWriter.AddAnnotation(
				check.WithDescriptor(message),
				check.WithMessagef("%s projects %q with neither a column nor a lookup, or with both; a projected row finds its resource one way.", message.FullName(), projection.GetTable()),
			)
		case byLookup && (projection.GetLookup() == "" || projection.GetVia() == ""):
			responseWriter.AddAnnotation(
				check.WithDescriptor(message),
				check.WithMessagef("%s projects %q by lookup, which names both lookup, the column on %q, and via, the column on the projected row.", message.FullName(), projection.GetTable(), option.GetTable()),
			)
		case byColumn && projection.GetWhere() != "":
			responseWriter.AddAnnotation(
				check.WithDescriptor(message),
				check.WithMessagef("%s projects %q by column and gives a where, which only a lookup runs.", message.FullName(), projection.GetTable()),
			)
		}
	}

	return nil
}

// checkRows holds watch.v1.Row's oneof to the resource option: every arm is a message carrying
// it, every message carrying it has an arm, and a skipped field number is reserved.
func checkRows(_ context.Context, responseWriter check.ResponseWriter, request check.Request) error {
	var (
		row       protoreflect.MessageDescriptor
		resources []protoreflect.MessageDescriptor
	)

	for message := range messagesUnderCheck(request) {
		if message.FullName() == rowMessage {
			row = message
		}

		if readResource(message) != nil {
			resources = append(resources, message)
		}
	}

	if row == nil {
		for _, resource := range resources {
			responseWriter.AddAnnotation(
				check.WithDescriptor(resource),
				check.WithMessagef("%s carries (annotation.v1.resource) and there is no %s to carry it on the watch.", resource.FullName(), rowMessage),
			)
		}

		return nil
	}

	if row.Oneofs().Len() != 1 {
		responseWriter.AddAnnotation(
			check.WithDescriptor(row),
			check.WithMessagef("%s declares %d oneofs, and it is one oneof with an arm per resource.", rowMessage, row.Oneofs().Len()),
		)

		return nil
	}

	arms := row.Oneofs().Get(0).Fields()
	carried := map[protoreflect.FullName]bool{}
	highest := protoreflect.FieldNumber(0)

	for i := range arms.Len() {
		arm := arms.Get(i)
		highest = max(highest, arm.Number())

		if arm.Kind() != protoreflect.MessageKind || readResource(arm.Message()) == nil {
			responseWriter.AddAnnotation(
				check.WithDescriptor(arm),
				check.WithMessagef("%q is an arm of %s and is not a message carrying (annotation.v1.resource), so nothing says which table it is a row of.", arm.Name(), rowMessage),
			)

			continue
		}

		carried[arm.Message().FullName()] = true
	}

	for _, resource := range resources {
		if !carried[resource.FullName()] {
			responseWriter.AddAnnotation(
				check.WithDescriptor(resource),
				check.WithMessagef("%s carries (annotation.v1.resource) for the table %q and %s has no arm for it, so the watch cannot carry its rows.", resource.FullName(), readResource(resource).GetTable(), rowMessage),
			)
		}
	}

	for number := protoreflect.FieldNumber(1); number < highest; number++ {
		if arms.ByNumber(number) == nil && !row.ReservedRanges().Has(number) {
			responseWriter.AddAnnotation(
				check.WithDescriptor(row),
				check.WithMessagef("%s skips field number %d, so reserve it.", rowMessage, number),
			)
		}
	}

	return nil
}

// messagesUnderCheck walks every message weave declares itself, nested ones included.
func messagesUnderCheck(request check.Request) func(func(protoreflect.MessageDescriptor) bool) {
	return func(yield func(protoreflect.MessageDescriptor) bool) {
		for _, file := range request.FileDescriptors() {
			if file.IsImport() {
				continue
			}

			if !walkMessages(file.ProtoreflectFileDescriptor().Messages(), yield) {
				return
			}
		}
	}
}

func walkMessages(messages protoreflect.MessageDescriptors, yield func(protoreflect.MessageDescriptor) bool) bool {
	for i := range messages.Len() {
		message := messages.Get(i)
		if !yield(message) || !walkMessages(message.Messages(), yield) {
			return false
		}
	}

	return true
}

// readResource reads the option, nil when the message carries none.
func readResource(message protoreflect.MessageDescriptor) *annotationv1.Resource {
	opts, ok := message.Options().(*descriptorpb.MessageOptions)
	if !ok || !proto.HasExtension(opts, annotationv1.E_Resource) {
		return nil
	}

	resource, _ := proto.GetExtension(opts, annotationv1.E_Resource).(*annotationv1.Resource)

	return resource
}
