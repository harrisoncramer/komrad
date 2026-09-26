package bufcheck

import (
	"context"

	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func checkFieldTypeNoStutter(
	_ context.Context,
	responseWriter check.ResponseWriter,
	request check.Request,
	field protoreflect.FieldDescriptor,
) error {
	if field.IsList() || field.IsMap() || field.Kind() != protoreflect.MessageKind {
		return nil
	}

	if !ownedFiles(request)[field.Message().ParentFile().Path()] {
		return nil
	}

	if field.Message().Fields().ByName(field.Name()) == nil {
		return nil
	}

	responseWriter.AddAnnotation(
		check.WithDescriptor(field),
		check.WithMessagef(
			"%q on %s is a %s, which declares a field named %q too. Flatten one into the other.",
			field.Name(),
			field.ContainingMessage().Name(),
			field.Message().Name(),
			field.Name(),
		),
	)

	return nil
}
