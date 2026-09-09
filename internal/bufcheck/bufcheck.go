// Package bufcheck holds the lint rules buf enforces on weave's own protobufs.
package bufcheck

import (
	"context"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	// ReadRuleID is the rule requiring a method that only reads to say so.
	ReadRuleID = "RPC_READ_NO_SIDE_EFFECTS"
	// MutationRuleID is the rule requiring a method that can be called twice to say so.
	MutationRuleID = "RPC_MUTATION_IDEMPOTENT"
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
			"%q is named for %s, so it must declare \"option idempotency_level = %s;\". The MCP hints and the CLI both read that option, and without it the method reaches every client as one that mutates the panel and cannot be retried.",
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
