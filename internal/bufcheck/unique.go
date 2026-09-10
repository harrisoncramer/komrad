package bufcheck

import (
	"context"
	"slices"
	"strings"

	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const emptyMessage = "google.protobuf.Empty"

func checkTypesUnique(_ context.Context, responseWriter check.ResponseWriter, request check.Request) error {
	callers := map[protoreflect.FullName][]string{}
	for method := range methodsUnderCheck(request) {
		for _, message := range []protoreflect.MessageDescriptor{method.Input(), method.Output()} {
			if message.FullName() == emptyMessage {
				continue
			}

			caller := string(method.FullName())
			if !slices.Contains(callers[message.FullName()], caller) {
				callers[message.FullName()] = append(callers[message.FullName()], caller)
			}
		}
	}

	for method := range methodsUnderCheck(request) {
		if method.Input().FullName() == method.Output().FullName() && method.Input().FullName() != emptyMessage {
			responseWriter.AddAnnotation(
				check.WithDescriptor(method),
				check.WithMessagef(
					"%q takes and returns the same type %q, so neither can gain a field without changing the other.",
					method.Name(),
					method.Input().FullName(),
				),
			)

			continue
		}

		for _, message := range []protoreflect.MessageDescriptor{method.Input(), method.Output()} {
			others := otherThan(callers[message.FullName()], string(method.FullName()))
			if len(others) == 0 {
				continue
			}

			responseWriter.AddAnnotation(
				check.WithDescriptor(method),
				check.WithMessagef(
					"%q shares the type %q with %s, so a field one of them needs is a field the others grow too. Give each method its own type, or return google.protobuf.Empty where there is nothing to say.",
					method.Name(),
					message.FullName(),
					strings.Join(others, ", "),
				),
			)
		}
	}

	return nil
}

func methodsUnderCheck(request check.Request) func(func(protoreflect.MethodDescriptor) bool) {
	return func(yield func(protoreflect.MethodDescriptor) bool) {
		for _, file := range request.FileDescriptors() {
			if file.IsImport() {
				continue
			}

			services := file.ProtoreflectFileDescriptor().Services()
			for i := range services.Len() {
				methods := services.Get(i).Methods()
				for j := range methods.Len() {
					if !yield(methods.Get(j)) {
						return
					}
				}
			}
		}
	}
}

func otherThan(callers []string, self string) []string {
	others := make([]string, 0, len(callers))
	for _, caller := range callers {
		if caller != self {
			others = append(others, caller)
		}
	}

	return others
}
