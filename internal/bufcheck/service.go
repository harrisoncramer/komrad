package bufcheck

import (
	"context"

	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func checkOneService(
	_ context.Context,
	responseWriter check.ResponseWriter,
	_ check.Request,
	service protoreflect.ServiceDescriptor,
) error {
	file := service.ParentFile()
	if file.Services().Len() < 2 {
		return nil
	}

	responseWriter.AddAnnotation(
		check.WithDescriptor(service),
		check.WithMessagef(
			"%s declares %d services. One service to a file, so give each of these its own.",
			file.Path(),
			file.Services().Len(),
		),
	)

	return nil
}
