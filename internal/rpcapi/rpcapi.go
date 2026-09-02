// Package rpcapi reads weave's own proto descriptors at runtime, so the CLI and the MCP server
// are generated from the same list of methods the handlers implement.
//
// It is what makes adding an RPC enough on its own. The method's path, its request fields and
// their validation rules, and the annotations saying whether it only reads and whether it is
// safe to retry all come from the descriptor, so a new endpoint arrives as a CLI subcommand and
// an MCP tool without either being written by hand.
package rpcapi

import (
	"iter"

	hookv1 "weave/proto/gen/hook/v1"
	agentv1 "weave/proto/gen/agent/v1"
	annotationv1 "weave/proto/gen/annotation/v1"
	configv1 "weave/proto/gen/config/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

var files = []protoreflect.FileDescriptor{
	agentv1.File_agent_v1_agent_proto,
	workflowv1.File_workflow_v1_workflow_proto,
	schedulev1.File_schedule_v1_schedule_proto,
	configv1.File_config_v1_config_proto,
	hookv1.File_hook_v1_hook_proto,
}

type Method struct {
	Service    protoreflect.ServiceDescriptor
	Method     protoreflect.MethodDescriptor
	Annotation *annotationv1.ToolAnnotation
}

// GetPath returns where the method is served, which is the Connect route for a unary call.
func (m Method) GetPath() string {
	return "/" + string(m.Service.FullName()) + "/" + string(m.Method.Name())
}

// GetMethods iterates every annotated method across every served service, in the order the
// protos declare them.
func GetMethods() iter.Seq[Method] {
	return func(yield func(Method) bool) {
		for _, file := range files {
			for s := range file.Services().Len() {
				service := file.Services().Get(s)

				for m := range service.Methods().Len() {
					method := service.Methods().Get(m)

					annotation := GetToolAnnotation(method)
					if annotation == nil {
						continue
					}

					if !yield(Method{
						Service:    service,
						Method:     method,
						Annotation: annotation,
					}) {
						return
					}
				}
			}
		}
	}
}

// GetToolAnnotation returns the method's tool annotation, nil when it has none and so is
// not exposed.
func GetToolAnnotation(method protoreflect.MethodDescriptor) *annotationv1.ToolAnnotation {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || !proto.HasExtension(opts, annotationv1.E_Tool) {
		return nil
	}

	annotation, ok := proto.GetExtension(opts, annotationv1.E_Tool).(*annotationv1.ToolAnnotation)
	if !ok || annotation.GetToolName() == "" {
		return nil
	}

	return annotation
}

// GetRules returns the validation rules declared on a field, nil when it carries none.
func GetRules(field protoreflect.FieldDescriptor) *validate.FieldRules {
	opts, ok := field.Options().(*descriptorpb.FieldOptions)
	if !ok || !proto.HasExtension(opts, validate.E_Field) {
		return nil
	}

	rules, ok := proto.GetExtension(opts, validate.E_Field).(*validate.FieldRules)
	if !ok {
		return nil
	}

	return rules
}

// IsReadOnly reports whether calling the method changes nothing, taking the annotation's
// override when it carries one and the method's idempotency level otherwise.
func IsReadOnly(method protoreflect.MethodDescriptor, annotation *annotationv1.ToolAnnotation) bool {
	if annotation != nil && annotation.ReadOnly != nil {
		return annotation.GetReadOnly()
	}

	return getIdempotencyLevel(method) == descriptorpb.MethodOptions_NO_SIDE_EFFECTS
}

// IsIdempotent reports whether calling the method twice lands where calling it once does,
// taking the annotation's override when it carries one.
func IsIdempotent(method protoreflect.MethodDescriptor, annotation *annotationv1.ToolAnnotation) bool {
	if annotation != nil && annotation.Idempotent != nil {
		return annotation.GetIdempotent()
	}

	level := getIdempotencyLevel(method)

	return level == descriptorpb.MethodOptions_IDEMPOTENT || level == descriptorpb.MethodOptions_NO_SIDE_EFFECTS
}

// IsRequired reports whether the service would refuse the field left empty, which is what
// the validation rules say rather than what proto3 field presence says.
func IsRequired(field protoreflect.FieldDescriptor) bool {
	rules := GetRules(field)
	if rules == nil || field.HasOptionalKeyword() {
		return false
	}

	if rules.GetRequired() {
		return true
	}

	return rules.GetString().GetMinLen() > 0 || rules.GetRepeated().GetMinItems() > 0
}

func getIdempotencyLevel(method protoreflect.MethodDescriptor) descriptorpb.MethodOptions_IdempotencyLevel {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok {
		return descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN
	}

	return opts.GetIdempotencyLevel()
}
