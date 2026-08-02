// Package rpcapi is the set of RPCs exposed to callers outside this process, which is
// every method carrying a tool annotation. The MCP server turns them into tools and the
// CLI into subcommands, and both read the surface from here so the two cannot drift.
package rpcapi

import (
	"iter"

	actionv1 "weave/proto/gen/action/v1"
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

// files are the protos whose services are served. A service missing from here is invisible
// outside the process whatever its methods are annotated with.
var files = []protoreflect.FileDescriptor{
	agentv1.File_agent_v1_agent_proto,
	workflowv1.File_workflow_v1_workflow_proto,
	schedulev1.File_schedule_v1_schedule_proto,
	configv1.File_config_v1_config_proto,
	actionv1.File_action_v1_action_proto,
}

// Method is one exposed RPC: the service to address it to, the method itself, and the
// annotation naming and describing it.
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

					if !yield(Method{Service: service, Method: method, Annotation: annotation}) {
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

	// CEL rules are not read, since what they accept is only knowable by running them, so a
	// field guarded solely by one is advertised as optional and refused by the server.
	return rules.GetString().GetMinLen() > 0 || rules.GetRepeated().GetMinItems() > 0
}

func getIdempotencyLevel(method protoreflect.MethodDescriptor) descriptorpb.MethodOptions_IdempotencyLevel {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok {
		return descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN
	}

	return opts.GetIdempotencyLevel()
}
