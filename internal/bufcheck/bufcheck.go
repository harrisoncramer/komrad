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
				Purpose: "Checks that a field naming an id on a request carries a validation rule refusing it empty.",
				Type:    check.RuleTypeLint,
				Handler: checkutil.NewMethodRuleHandler(checkRequestIDs, checkutil.WithoutImports()),
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
	_ check.Request,
	method protoreflect.MethodDescriptor,
) error {
	fields := method.Input().Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		if !namesAnID(field) || refusesEmpty(field) {
			continue
		}

		responseWriter.AddAnnotation(
			check.WithDescriptor(field),
			check.WithMessagef(
				"%q names an id on %s, so it must carry a validation rule refusing it empty, such as (buf.validate.field).string.min_len = 1. The CLI marks a flag required and the MCP schema marks a field required from that rule, so without it both surfaces call the field optional while %s still needs it.",
				field.Name(),
				method.Input().Name(),
				method.Name(),
			),
		)
	}

	return nil
}

func namesAnID(field protoreflect.FieldDescriptor) bool {
	name := string(field.Name())

	return name == "id" || name == "ids" || strings.HasSuffix(name, "_id") || strings.HasSuffix(name, "_ids")
}

func refusesEmpty(field protoreflect.FieldDescriptor) bool {
	opts, ok := field.Options().(*descriptorpb.FieldOptions)
	if !ok || !proto.HasExtension(opts, validate.E_Field) {
		return false
	}

	rules, ok := proto.GetExtension(opts, validate.E_Field).(*validate.FieldRules)
	if !ok {
		return false
	}

	return rules.GetRequired() || rules.GetString().GetMinLen() > 0 || rules.GetRepeated().GetMinItems() > 0
}

func requireLevel(
	responseWriter check.ResponseWriter,
	method protoreflect.MethodDescriptor,
	prefixes []string,
	kind string,
	want descriptorpb.MethodOptions_IdempotencyLevel,
) error {
	if !namedFor(method, prefixes) {
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

func namedFor(method protoreflect.MethodDescriptor, prefixes []string) bool {
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
