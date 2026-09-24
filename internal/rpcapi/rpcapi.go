// Package rpcapi reads komrade's own proto descriptors at runtime, so the CLI and the MCP server
// are generated from the same list of methods the handlers implement.
//
// It is what makes adding an RPC enough on its own. The method's path, its request fields and
// their validation rules, and the annotations saying whether it only reads and whether it is
// safe to retry all come from the descriptor, so a new endpoint arrives as a CLI subcommand and
// an MCP tool without either being written by hand.
package rpcapi

import (
	"fmt"
	"iter"
	"slices"

	agentv1 "komrade/proto/gen/agent/v1"
	annotationv1 "komrade/proto/gen/annotation/v1"
	configv1 "komrade/proto/gen/config/v1"
	doctorv1 "komrade/proto/gen/doctor/v1"
	forgev1 "komrade/proto/gen/forge/v1"
	hookv1 "komrade/proto/gen/hook/v1"
	logv1 "komrade/proto/gen/log/v1"
	pullrequestv1 "komrade/proto/gen/pullrequest/v1"
	schedulev1 "komrade/proto/gen/schedule/v1"
	watchv1 "komrade/proto/gen/watch/v1"
	workflowv1 "komrade/proto/gen/workflow/v1"
	worktreev1 "komrade/proto/gen/worktree/v1"

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
	doctorv1.File_doctor_v1_doctor_proto,
	hookv1.File_hook_v1_hook_proto,
	logv1.File_log_v1_log_proto,
	forgev1.File_forge_v1_forge_proto,
	pullrequestv1.File_pullrequest_v1_pullrequest_proto,
	worktreev1.File_worktree_v1_worktree_proto,
	watchv1.File_watch_v1_watch_proto,
}

type Method struct {
	Service  protoreflect.ServiceDescriptor
	Method   protoreflect.MethodDescriptor
	Surfaces *annotationv1.Surfaces
}

// GetPath returns where the method is served, which for a unary call is its Connect route.
func (m Method) GetPath() string {
	return "/" + string(m.Service.FullName()) + "/" + string(m.Method.Name())
}

// GetMethods iterates every method the CLI reaches across every served service, in the order
// the protos declare them.
func GetMethods() iter.Seq[Method] {
	return func(yield func(Method) bool) {
		for _, file := range files {
			for s := range file.Services().Len() {
				service := file.Services().Get(s)

				for m := range service.Methods().Len() {
					method := service.Methods().Get(m)

					surfaces := GetSurfaces(method)
					if !surfaces.GetCli() || surfaces.GetName() == "" {
						continue
					}

					if !yield(Method{
						Service:  service,
						Method:   method,
						Surfaces: surfaces,
					}) {
						return
					}
				}
			}
		}
	}
}

// GetSurfaces returns which generated callers reach the method, nil when it declares none.
func GetSurfaces(method protoreflect.MethodDescriptor) *annotationv1.Surfaces {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || !proto.HasExtension(opts, annotationv1.E_Surfaces) {
		return nil
	}

	surfaces, ok := proto.GetExtension(opts, annotationv1.E_Surfaces).(*annotationv1.Surfaces)
	if !ok {
		return nil
	}

	return surfaces
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

// IsReadOnly reports whether calling the method changes nothing, which its idempotency level says.
func IsReadOnly(method protoreflect.MethodDescriptor) bool {
	return getIdempotencyLevel(method) == descriptorpb.MethodOptions_NO_SIDE_EFFECTS
}

// IsIdempotent reports whether calling the method twice lands where calling it once does,
// taking the annotation's override when it carries one.
func IsIdempotent(method protoreflect.MethodDescriptor, surfaces *annotationv1.Surfaces) bool {
	if surfaces != nil && surfaces.Idempotent != nil {
		return surfaces.GetIdempotent()
	}

	level := getIdempotencyLevel(method)

	return level == descriptorpb.MethodOptions_IDEMPOTENT || level == descriptorpb.MethodOptions_NO_SIDE_EFFECTS
}

// IsDestructive reports whether the call can take something away.
func IsDestructive(method protoreflect.MethodDescriptor, surfaces *annotationv1.Surfaces) bool {
	if IsReadOnly(method) {
		return false
	}

	if surfaces != nil && surfaces.Destructive != nil {
		return surfaces.GetDestructive()
	}

	return true
}

// ServesRole reports whether a tool is offered to a caller holding the role, every tool being
// offered to a caller holding none.
func ServesRole(surfaces *annotationv1.Surfaces, role annotationv1.Role) bool {
	if role == annotationv1.Role_ROLE_UNSPECIFIED {
		return true
	}

	return slices.Contains(surfaces.GetRoles(), role)
}

// EnumNames is what an enum's values are spelled with where a caller has to name one, leaving
// out the zero value the field already holds.
func EnumNames(values protoreflect.EnumDescriptor) []string {
	names := make([]string, 0, values.Values().Len())
	for i := range values.Values().Len() {
		value := values.Values().Get(i)
		if value.Number() == 0 {
			continue
		}

		names = append(names, string(value.Name()))
	}

	return names
}

// IsRequired reports whether the service would refuse the field left empty, which the validation
// rules decide rather than proto3 field presence.
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

// DescribeRules spells out what the service refuses of a field, in the order a reader weighs them.
func DescribeRules(field protoreflect.FieldDescriptor) []string {
	var parts []string
	if IsRequired(field) {
		parts = append(parts, "required")
	}

	rules := GetRules(field)
	if text := rules.GetString(); text != nil {
		if limit := text.GetMaxLen(); limit > 0 {
			parts = append(parts, fmt.Sprintf("at most %d characters", limit))
		}
		if pattern := text.GetPattern(); pattern != "" {
			parts = append(parts, "matching "+pattern)
		}
	}

	if number := rules.GetInt32(); number != nil {
		if number.GetGte() > 0 || number.GetLte() > 0 {
			parts = append(parts, fmt.Sprintf("between %d and %d", number.GetGte(), number.GetLte()))
		}
	}

	return parts
}

func getIdempotencyLevel(method protoreflect.MethodDescriptor) descriptorpb.MethodOptions_IdempotencyLevel {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok {
		return descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN
	}

	return opts.GetIdempotencyLevel()
}
