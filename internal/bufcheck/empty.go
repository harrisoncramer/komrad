package bufcheck

import (
	"context"

	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func checkNotEmpty(
	_ context.Context,
	responseWriter check.ResponseWriter,
	_ check.Request,
	message protoreflect.MessageDescriptor,
) error {
	if message.Fields().Len() > 0 {
		return nil
	}

	responseWriter.AddAnnotation(
		check.WithDescriptor(message),
		check.WithMessagef(
			"%s declares no fields. Use google.protobuf.Empty instead.",
			message.FullName(),
		),
	)

	return nil
}
