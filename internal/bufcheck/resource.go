package bufcheck

import (
	"context"
	"strings"
	"unicode"

	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func checkGetReturnsResource(
	_ context.Context,
	responseWriter check.ResponseWriter,
	request check.Request,
	method protoreflect.MethodDescriptor,
) error {
	noun, ok := strings.CutPrefix(string(method.Name()), "Get")
	if !ok || noun == "" {
		return nil
	}

	resource := findOwnedMessage(request, protoreflect.Name(noun))
	if resource == nil {
		return nil
	}

	want := snakeCase(noun)
	field := method.Output().Fields().ByName(protoreflect.Name(want))

	if field != nil && !field.IsList() && !field.IsMap() &&
		field.Kind() == protoreflect.MessageKind && field.Message().FullName() == resource.FullName() {
		return nil
	}

	responseWriter.AddAnnotation(
		check.WithDescriptor(method.Output()),
		check.WithMessagef(
			"%s must carry %s as a field named %q.",
			method.Output().Name(),
			resource.Name(),
			want,
		),
	)

	return nil
}

func findOwnedMessage(request check.Request, name protoreflect.Name) protoreflect.MessageDescriptor {
	for _, file := range request.FileDescriptors() {
		if file.IsImport() {
			continue
		}

		if message := file.ProtoreflectFileDescriptor().Messages().ByName(name); message != nil {
			return message
		}
	}

	return nil
}

func snakeCase(name string) string {
	var out strings.Builder

	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				out.WriteByte('_')
			}

			r = unicode.ToLower(r)
		}

		out.WriteRune(r)
	}

	return out.String()
}
