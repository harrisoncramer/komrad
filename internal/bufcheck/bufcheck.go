// Package bufcheck holds the lint rules buf enforces on weave's own protobufs.
package bufcheck

import (
	"context"
	"strings"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	// ReadRuleID is the rule requiring a method that only reads to say so.
	ReadRuleID = "RPC_READ_NO_SIDE_EFFECTS"
	// MutationRuleID is the rule requiring a method that can be called twice to say so.
	MutationRuleID = "RPC_MUTATION_IDEMPOTENT"
	// RequestIDRuleID is the rule requiring a request's id fields to refuse the empty string.
	RequestIDRuleID = "RPC_REQUEST_ID_REFUSES_EMPTY"
	// TypesUniqueRuleID is the rule holding a named request or response type to one method.
	TypesUniqueRuleID = "RPC_NAMED_TYPES_UNIQUE"
	// RequestEnumRuleID is the rule requiring a request's enum fields to refuse an undefined value.
	RequestEnumRuleID = "RPC_REQUEST_ENUM_DEFINED_ONLY"
	// OneServiceRuleID is the rule giving each service a file of its own.
	OneServiceRuleID = "SERVICE_ONE_PER_FILE"
	// SurfacesRuleID is the rule requiring a method to say who reaches it, consistently.
	SurfacesRuleID = "RPC_DECLARES_SURFACES"
	// ResourceRuleID is the rule holding a resource option to its shape.
	ResourceRuleID = "RESOURCE_NAMES_ITS_TABLE"
	// RowsRuleID is the rule holding the watch's Row oneof to the resource option.
	RowsRuleID = "WATCH_ROW_PER_RESOURCE"
)

var (
	readPrefixes     = []string{"Get", "List", "Search"}
	mutationPrefixes = []string{"Archive", "Delete", "Edit", "Mark", "Restore", "Resume", "Set", "Update"}

	// Spec is the plugin buf loads these rules from.
	Spec = &check.Spec{
		Rules: []*check.RuleSpec{
			{
				ID:      ReadRuleID,
				Default: true,
				Purpose: "Checks that a method named for a read declares option idempotency_level = NO_SIDE_EFFECTS.",
				Type:    check.RuleTypeLint,
				Handler: checkutil.NewMethodRuleHandler(checkRead, checkutil.WithoutImports()),
			},
			{
				ID:      MutationRuleID,
				Default: true,
				Purpose: "Checks that a method named for a repeatable write declares option idempotency_level = IDEMPOTENT.",
				Type:    check.RuleTypeLint,
				Handler: checkutil.NewMethodRuleHandler(checkMutation, checkutil.WithoutImports()),
			},
			{
				ID:      RequestIDRuleID,
				Default: true,
				Purpose: "Checks that a field naming an id that a request reaches carries a validation rule refusing it empty.",
				Type:    check.RuleTypeLint,
				Handler: checkutil.NewMethodRuleHandler(checkRequestIDs, checkutil.WithoutImports()),
			},
			{
				ID:      RequestEnumRuleID,
				Default: true,
				Purpose: "Checks that an enum field a request reaches carries a validation rule refusing a value the enum does not define.",
				Type:    check.RuleTypeLint,
				Handler: checkutil.NewMethodRuleHandler(checkRequestEnums, checkutil.WithoutImports()),
			},
			{
				ID:      SurfacesRuleID,
				Default: true,
				Purpose: "Checks that a method declares (annotation.v1.surfaces) and that what it declares is consistent: a tool has a command, a surface that is on has a name and a description, and a surface that is off has a reason.",
				Type:    check.RuleTypeLint,
				Handler: checkutil.NewMethodRuleHandler(checkSurfaces, checkutil.WithoutImports()),
			},
			{
				ID:      OneServiceRuleID,
				Default: true,
				Purpose: "Checks that a file declares at most one service.",
				Type:    check.RuleTypeLint,
				Handler: checkutil.NewServiceRuleHandler(checkOneService, checkutil.WithoutImports()),
			},
			{
				ID:      ResourceRuleID,
				Default: true,
				Purpose: "Checks that a message carrying (annotation.v1.resource) names its table, and that each projection names its table and one way of finding the resource: a column holding its key, or a lookup with a via.",
				Type:    check.RuleTypeLint,
				Handler: checkutil.NewMessageRuleHandler(checkResource, checkutil.WithoutImports()),
			},
			{
				ID:      RowsRuleID,
				Default: true,
				Purpose: "Checks that every arm of watch.v1.Row is a message carrying (annotation.v1.resource), that every message carrying it has an arm, and that a skipped field number is reserved.",
				Type:    check.RuleTypeLint,
				Handler: check.RuleHandlerFunc(checkRows),
			},
			{
				ID:      TypesUniqueRuleID,
				Default: true,
				Purpose: "Checks that a named request or response type belongs to exactly one method, google.protobuf.Empty aside.",
				Type:    check.RuleTypeLint,
				Handler: check.RuleHandlerFunc(checkTypesUnique),
			},
		},
	}
)

func checkRead(
	_ context.Context,
	responseWriter check.ResponseWriter,
	_ check.Request,
	method protoreflect.MethodDescriptor,
) error {
	return requireLevel(responseWriter, method, readPrefixes, "a read", descriptorpb.MethodOptions_NO_SIDE_EFFECTS)
}

func checkMutation(
	_ context.Context,
	responseWriter check.ResponseWriter,
	_ check.Request,
	method protoreflect.MethodDescriptor,
) error {
	return requireLevel(responseWriter, method, mutationPrefixes, "a write that can be repeated", descriptorpb.MethodOptions_IDEMPOTENT)
}

func checkRequestIDs(
	_ context.Context,
	responseWriter check.ResponseWriter,
	request check.Request,
	method protoreflect.MethodDescriptor,
) error {
	for message, field := range requestFields(request, method) {
		if !namesAnID(field) || refusesEmpty(field) {
			continue
		}

		responseWriter.AddAnnotation(
			check.WithDescriptor(field),
			check.WithMessagef(
				"%q names an id on %s, so it must carry a validation rule refusing it empty, such as (buf.validate.field).string.min_len = 1. The CLI marks a flag required and the MCP schema marks a field required from that rule, so without it both surfaces call the field optional while %s still needs it.",
				field.Name(),
				message.Name(),
				method.Name(),
			),
		)
	}

	return nil
}

func checkRequestEnums(
	_ context.Context,
	responseWriter check.ResponseWriter,
	request check.Request,
	method protoreflect.MethodDescriptor,
) error {
	for message, field := range requestFields(request, method) {
		if !doesHoldEnum(field) || isDefinedOnly(field) {
			continue
		}

		responseWriter.AddAnnotation(
			check.WithDescriptor(field),
			check.WithMessagef(
				"%q on %s must carry %s. Without it a number the enum does not define reaches %s as a value no switch over it expects.",
				field.Name(),
				message.Name(),
				buildDefinedOnlySpelling(field),
				method.Name(),
			),
		)
	}

	return nil
}

func doesHoldEnum(field protoreflect.FieldDescriptor) bool {
	if field.IsMap() {
		return field.MapValue().Kind() == protoreflect.EnumKind
	}

	return field.Kind() == protoreflect.EnumKind
}

func buildDefinedOnlySpelling(field protoreflect.FieldDescriptor) string {
	switch {
	case field.IsMap():
		return "(buf.validate.field).map.values.enum.defined_only = true"
	case field.IsList():
		return "(buf.validate.field).repeated.items.enum.defined_only = true"
	default:
		return "(buf.validate.field).enum.defined_only = true"
	}
}

func isDefinedOnly(field protoreflect.FieldDescriptor) bool {
	rules := fieldRules(field)
	if rules == nil {
		return false
	}

	switch {
	case field.IsMap():
		return rules.GetMap().GetValues().GetEnum().GetDefinedOnly()
	case field.IsList():
		return rules.GetRepeated().GetItems().GetEnum().GetDefinedOnly()
	default:
		return rules.GetEnum().GetDefinedOnly()
	}
}

func namesAnID(field protoreflect.FieldDescriptor) bool {
	name := string(field.Name())

	return name == "id" || name == "ids" || strings.HasSuffix(name, "_id") || strings.HasSuffix(name, "_ids")
}

func refusesEmpty(field protoreflect.FieldDescriptor) bool {
	rules := fieldRules(field)

	return rules != nil &&
		(rules.GetRequired() || rules.GetString().GetMinLen() > 0 || rules.GetRepeated().GetMinItems() > 0)
}

func fieldRules(field protoreflect.FieldDescriptor) *validate.FieldRules {
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

func requireLevel( //nolint:revive // a params struct is the follow-up
	responseWriter check.ResponseWriter,
	method protoreflect.MethodDescriptor,
	prefixes []string,
	kind string,
	want descriptorpb.MethodOptions_IdempotencyLevel,
) error {
	if !isNamedFor(method, prefixes) {
		return nil
	}

	if idempotencyLevel(method) == want {
		return nil
	}

	responseWriter.AddAnnotation(
		check.WithDescriptor(method),
		check.WithMessagef(
			"%q is named for %s, so it must declare \"option idempotency_level = %s;\". The MCP tool hints derive from that option, so without it the method reaches every client as one that changes the panel and cannot be retried.",
			method.Name(),
			kind,
			want,
		),
	)

	return nil
}

func isNamedFor(method protoreflect.MethodDescriptor, prefixes []string) bool {
	name := string(method.Name())

	for _, prefix := range prefixes {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}

		if rest == "" || rest[0] >= 'A' && rest[0] <= 'Z' {
			return true
		}
	}

	return false
}

func idempotencyLevel(method protoreflect.MethodDescriptor) descriptorpb.MethodOptions_IdempotencyLevel {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok {
		return descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN
	}

	return opts.GetIdempotencyLevel()
}
