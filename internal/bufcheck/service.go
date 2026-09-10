package bufcheck

import (
	"context"
	"strings"

	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func checkServiceName(
	_ context.Context,
	responseWriter check.ResponseWriter,
	_ check.Request,
	service protoreflect.ServiceDescriptor,
) error {
	file := service.ParentFile()
	if file.Services().Len() > 1 {
		responseWriter.AddAnnotation(
			check.WithDescriptor(service),
			check.WithMessagef(
				"%s declares %d services, and weave's tooling reads the first one it finds. Give each service a file of its own.",
				file.Path(),
				file.Services().Len(),
			),
		)

		return nil
	}

	want := serviceNameFor(file.Package())
	if string(service.Name()) == want {
		return nil
	}

	responseWriter.AddAnnotation(
		check.WithDescriptor(service),
		check.WithMessagef(
			"the service in package %s should be named %q rather than %q, so its name says which package answers a call.",
			file.Package(),
			want,
			service.Name(),
		),
	)

	return nil
}

func serviceNameFor(pkg protoreflect.FullName) string {
	domain, _, found := strings.Cut(string(pkg), ".")
	if !found {
		domain = string(pkg)
	}

	var name strings.Builder
	for _, word := range strings.Split(domain, "_") {
		if word == "" {
			continue
		}

		name.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}

	name.WriteString("Service")

	return name.String()
}
