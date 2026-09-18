package bufcheck

import (
	"context"

	annotationv1 "weave/proto/gen/annotation/v1"

	"buf.build/go/bufplugin/check"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func checkOneSurface(
	_ context.Context,
	responseWriter check.ResponseWriter,
	_ check.Request,
	method protoreflect.MethodDescriptor,
) error {
	tool, panelOnly := surfaces(method)

	switch {
	case tool == nil && panelOnly == nil:
		responseWriter.AddAnnotation(
			check.WithDescriptor(method),
			check.WithMessagef(
				"%q declares neither (annotation.v1.tool) nor (annotation.v1.panel_only), so it is served and reachable from nothing: the CLI and the MCP server are both generated from the tool annotation, and a method carrying none exists for no caller. Give it a tool annotation, or say with panel_only why the panel is to keep it.",
				method.Name(),
			),
		)
	case tool != nil && panelOnly != nil:
		responseWriter.AddAnnotation(
			check.WithDescriptor(method),
			check.WithMessagef(
				"%q declares both (annotation.v1.tool) and (annotation.v1.panel_only), which say opposite things about the same method. Drop whichever of the two is wrong.",
				method.Name(),
			),
		)
	case panelOnly != nil && panelOnly.GetReason() == "":
		responseWriter.AddAnnotation(
			check.WithDescriptor(method),
			check.WithMessagef(
				"%q declares (annotation.v1.panel_only) with no reason. The reason is the whole of what the annotation records, so without it the next reader cannot tell a deliberate omission from an oversight.",
				method.Name(),
			),
		)
	}

	return nil
}

// surfaces reads the two annotations a method may declare, each nil when it carries none.
func surfaces(method protoreflect.MethodDescriptor) (*annotationv1.ToolAnnotation, *annotationv1.PanelOnlyAnnotation) {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok {
		return nil, nil
	}

	var (
		tool      *annotationv1.ToolAnnotation
		panelOnly *annotationv1.PanelOnlyAnnotation
	)

	if proto.HasExtension(opts, annotationv1.E_Tool) {
		tool, _ = proto.GetExtension(opts, annotationv1.E_Tool).(*annotationv1.ToolAnnotation)
	}

	if proto.HasExtension(opts, annotationv1.E_PanelOnly) {
		panelOnly, _ = proto.GetExtension(opts, annotationv1.E_PanelOnly).(*annotationv1.PanelOnlyAnnotation)
	}

	return tool, panelOnly
}
