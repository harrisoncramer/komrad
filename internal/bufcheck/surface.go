package bufcheck

import (
	"context"

	annotationv1 "komrade/proto/gen/annotation/v1"

	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// checkSurfaces holds every method to the one way of saying who reaches it: the annotation is
// present, a tool never lacks a command, a surface that is on has a name and a description, a
// tool names the roles it is served to, and a surface that is off has a reason.
func checkSurfaces(
	_ context.Context,
	responseWriter check.ResponseWriter,
	_ check.Request,
	method protoreflect.MethodDescriptor,
) error {
	surfaces := readSurfaces(method)
	if surfaces == nil {
		responseWriter.AddAnnotation(
			check.WithDescriptor(method),
			check.WithMessagef(
				"%q declares no (annotation.v1.surfaces), so nothing says who reaches it: the CLI and the MCP server are both generated from that annotation, and a method carrying none is served for the panel alone without saying so. Declare it, with cli and mcp each true or false.",
				method.Name(),
			),
		)

		return nil
	}

	on := surfaces.GetCli() || surfaces.GetMcp()
	off := !surfaces.GetCli() || !surfaces.GetMcp()

	switch {
	case surfaces.GetMcp() && !surfaces.GetCli():
		responseWriter.AddAnnotation(
			check.WithDescriptor(method),
			check.WithMessagef(
				"%q is offered to a model as a tool but not to a script as a command. A tool is a command a model runs, so mcp implies cli; turn cli on or mcp off.",
				method.Name(),
			),
		)
	case on && surfaces.GetName() == "":
		responseWriter.AddAnnotation(
			check.WithDescriptor(method),
			check.WithMessagef(
				"%q is on a surface with no name, which is what the command and the tool are both generated under, so komrade serves it on neither.",
				method.Name(),
			),
		)
	case on && surfaces.GetDescription() == "":
		responseWriter.AddAnnotation(
			check.WithDescriptor(method),
			check.WithMessagef(
				"%q is on a surface with no description. That string is the whole of what a model is told about the call and all the help the CLI prints, so without it the method reaches its callers as a name and nothing else.",
				method.Name(),
			),
		)
	case surfaces.GetMcp() && len(surfaces.GetRoles()) == 0:
		responseWriter.AddAnnotation(
			check.WithDescriptor(method),
			check.WithMessagef(
				"%q is offered as a tool without naming the roles it is served to. A tool list is the largest fixed cost in an agent's context, so every tool says which of ROLE_IMPLEMENTER, ROLE_REVIEWER and ROLE_ORCHESTRATOR it is for; name all three where it is for all three.",
				method.Name(),
			),
		)
	case off && surfaces.GetReason() == "":
		responseWriter.AddAnnotation(
			check.WithDescriptor(method),
			check.WithMessagef(
				"%q leaves a surface off with no reason. The reason is what tells the next reader a deliberate omission from an oversight.",
				method.Name(),
			),
		)
	case !off && surfaces.GetReason() != "":
		responseWriter.AddAnnotation(
			check.WithDescriptor(method),
			check.WithMessagef(
				"%q is on every surface and yet gives a reason, which explains an omission that is not there. Drop the reason, or turn a surface off.",
				method.Name(),
			),
		)
	}

	return nil
}

// readSurfaces reads the annotation, nil when the method carries none.
func readSurfaces(method protoreflect.MethodDescriptor) *annotationv1.Surfaces {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || !proto.HasExtension(opts, annotationv1.E_Surfaces) {
		return nil
	}

	surfaces, _ := proto.GetExtension(opts, annotationv1.E_Surfaces).(*annotationv1.Surfaces)

	return surfaces
}
