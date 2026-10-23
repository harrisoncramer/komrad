package rpcapi

import (
	"context"
	"iter"
	"slices"
	"time"

	"komrad/internal/fail"
	"komrad/lib/protoenum"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	annotationv1 "komrad/proto/gen/annotation/v1"
	configv1 "komrad/proto/gen/config/v1"
	doctorv1 "komrad/proto/gen/doctor/v1"
	forgev1 "komrad/proto/gen/forge/v1"
	hookv1 "komrad/proto/gen/hook/v1"
	logv1 "komrad/proto/gen/log/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	taskv1.File_task_v1_task_proto,
}

var timestampName = (&timestamppb.Timestamp{}).ProtoReflect().Descriptor().FullName()

var awaitsHooks = map[string]bool{
	agentv1connect.AgentServiceCreateAgentProcedure:           true,
	agentv1connect.AgentServiceRequestReviewProcedure:         true,
	agentv1connect.AgentServiceAdoptProcedure:                 true,
	agentv1connect.AgentServiceStartOrchestratorProcedure:     true,
	agentv1connect.AgentServiceRetryAgentProcedure:            true,
	agentv1connect.AgentServiceRestartAgentProcedure:          true,
	agentv1connect.AgentServiceResumeAgentProcedure:           true,
	agentv1connect.AgentServiceArchiveAgentsProcedure:         true,
	agentv1connect.AgentServiceRestoreAgentsProcedure:         true,
	taskv1connect.TaskServiceArchiveTaskProcedure:             true,
	taskv1connect.TaskServiceDeleteTasksProcedure:             true,
	workflowv1connect.WorkflowServiceArchiveWorkflowProcedure: true,
	workflowv1connect.WorkflowServiceRestoreWorkflowProcedure: true,
	workflowv1connect.WorkflowServiceDeleteWorkflowProcedure:  true,
}

type Method struct {
	Service  protoreflect.ServiceDescriptor
	Method   protoreflect.MethodDescriptor
	Surfaces *annotationv1.Surfaces
}

// GetCallTimeout returns how long a caller waits on the method, or 0 for one the server alone bounds.
func (m Method) GetCallTimeout(fallback time.Duration) time.Duration {
	if awaitsHooks[m.GetPath()] {
		return 0
	}

	return fallback
}

// GetPath returns where the service serves the method, which for a unary call is its Connect route.
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

// getRules returns the validation rules declared on a field, nil when it carries none.
func getRules(field protoreflect.FieldDescriptor) *validate.FieldRules {
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

// ErrUnknownCaller is a call naming an agent komrad holds no row for.
var ErrUnknownCaller = fail.Denied("komrad knows no agent by that id")

// RoleOf answers what komrad launched the agent to do.
type RoleOf func(ctx context.Context, agentID uuid.UUID) (annotationv1.Role, error)

// ServesRole reports whether a method is open to a caller holding the role, every method being
// open to a caller holding none.
func ServesRole(surfaces *annotationv1.Surfaces, role annotationv1.Role) bool {
	if role == annotationv1.Role_ROLE_UNSPECIFIED {
		return true
	}

	return slices.Contains(surfaces.GetRoles(), role)
}

// EnumNames returns the spellings of the enum values the field accepts where a caller has to name one, leaving
// out the zero value the field already holds.
func EnumNames(field protoreflect.FieldDescriptor) []string {
	return xslice.Map(protoenum.AcceptedOf(field), protoenum.NameOf)
}

// IsTimestamp reports whether the field holds a google.protobuf.Timestamp.
func IsTimestamp(field protoreflect.FieldDescriptor) bool {
	return field.Kind() == protoreflect.MessageKind && field.Message().FullName() == timestampName
}

// IsRequired reports whether the server would refuse the field left empty, which the validation
// rules decide rather than proto3 field presence.
func IsRequired(field protoreflect.FieldDescriptor) bool {
	rules := getRules(field)
	if rules == nil || field.ContainingOneof() != nil {
		return false
	}

	if rules.GetRequired() {
		return true
	}

	text := rules.GetString()
	if in := text.GetIn(); len(in) > 0 && !slices.Contains(in, "") {
		return true
	}

	if field.Kind() == protoreflect.EnumKind && !field.IsList() && !protoenum.IsAccepted(field, 0) {
		return true
	}

	return text.GetMinLen() > 0 || text.GetUuid() || rules.GetRepeated().GetMinItems() > 0
}

// IsUUID reports whether the server accepts only a UUID in the field, its list items or its map
// values.
func IsUUID(field protoreflect.FieldDescriptor) bool {
	rules := getRules(field)
	switch {
	case field.IsMap():
		rules = rules.GetMap().GetValues()
	case field.IsList():
		rules = rules.GetRepeated().GetItems()
	}

	return rules.GetString().GetUuid() || rules.GetString().GetTuuid() || rules.GetBytes().GetUuid()
}

// IsOneofRequired reports whether the server refuses a request setting none of the oneof's fields.
func IsOneofRequired(oneof protoreflect.OneofDescriptor) bool {
	opts, ok := oneof.Options().(*descriptorpb.OneofOptions)
	if !ok || !proto.HasExtension(opts, validate.E_Oneof) {
		return false
	}

	rules, ok := proto.GetExtension(opts, validate.E_Oneof).(*validate.OneofRules)

	return ok && rules.GetRequired()
}

func getIdempotencyLevel(method protoreflect.MethodDescriptor) descriptorpb.MethodOptions_IdempotencyLevel {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok {
		return descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN
	}

	return opts.GetIdempotencyLevel()
}
