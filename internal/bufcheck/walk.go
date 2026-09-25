package bufcheck

import (
	"iter"

	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// requestFields walks the fields a request carries, descending into the messages komrad declares
// itself and stopping at the ones it imports.
func requestFields(
	request check.Request,
	method protoreflect.MethodDescriptor,
) iter.Seq2[protoreflect.MessageDescriptor, protoreflect.FieldDescriptor] {
	owned := ownedFiles(request)

	return func(yield func(protoreflect.MessageDescriptor, protoreflect.FieldDescriptor) bool) {
		seen := map[protoreflect.FullName]bool{}
		walkFields(method.Input(), owned, seen, yield)
	}
}

func walkFields(
	message protoreflect.MessageDescriptor,
	owned map[string]bool,
	seen map[protoreflect.FullName]bool,
	yield func(protoreflect.MessageDescriptor, protoreflect.FieldDescriptor) bool,
) bool {
	if seen[message.FullName()] || !owned[message.ParentFile().Path()] {
		return true
	}

	seen[message.FullName()] = true

	fields := message.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		if !yield(message, field) {
			return false
		}

		nested := nestedMessage(field)
		if nested == nil {
			continue
		}

		if !walkFields(nested, owned, seen, yield) {
			return false
		}
	}

	return true
}

func nestedMessage(field protoreflect.FieldDescriptor) protoreflect.MessageDescriptor {
	if field.IsMap() {
		if field.MapValue().Kind() != protoreflect.MessageKind {
			return nil
		}

		return field.MapValue().Message()
	}

	if field.Kind() != protoreflect.MessageKind {
		return nil
	}

	return field.Message()
}

func ownedFiles(request check.Request) map[string]bool {
	owned := make(map[string]bool)
	for _, file := range request.FileDescriptors() {
		if !file.IsImport() {
			owned[file.ProtoreflectFileDescriptor().Path()] = true
		}
	}

	return owned
}
