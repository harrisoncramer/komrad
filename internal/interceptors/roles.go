package interceptors

import (
	"context"
	"fmt"
	"strings"

	annotationv1 "komrad/proto/gen/annotation/v1"

	"komrad/internal/caller"
	"komrad/internal/rpcapi"

	"connectrpc.com/connect"
)

type rolesInterceptor struct {
	roleOf  rpcapi.RoleOf
	methods map[string]*annotationv1.Surfaces
}

// NewRoles builds the interceptor that refuses an agent a method its role is not listed on.
func NewRoles(roleOf rpcapi.RoleOf) connect.Interceptor {
	methods := make(map[string]*annotationv1.Surfaces)
	for method := range rpcapi.GetMethods() {
		if len(method.Surfaces.GetRoles()) > 0 {
			methods[method.GetPath()] = method.Surfaces
		}
	}

	return &rolesInterceptor{roleOf: roleOf, methods: methods}
}

func (r *rolesInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := r.authorize(ctx, req.Spec().Procedure); err != nil {
			return nil, err
		}

		return next(ctx, req)
	}
}

// WrapStreamingClient passes through on a service that opens no streams of its own.
func (r *rolesInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler refuses a stream the caller's role is not listed on before it opens.
func (r *rolesInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if err := r.authorize(ctx, conn.Spec().Procedure); err != nil {
			return err
		}

		return next(ctx, conn)
	}
}

func (r *rolesInterceptor) authorize(ctx context.Context, procedure string) error {
	surfaces, ok := r.methods[procedure]
	if !ok {
		return nil
	}

	agentID := caller.From(ctx).AgentID
	if agentID == "" {
		return nil
	}

	role := r.roleOf(ctx, agentID)
	if rpcapi.ServesRole(surfaces, role) {
		return nil
	}

	return connect.NewError(connect.CodePermissionDenied,
		fmt.Errorf("%s is not open to an agent in the %s role", surfaces.GetName(), roleName(role)))
}

func roleName(role annotationv1.Role) string {
	return strings.ToLower(strings.TrimPrefix(role.String(), "ROLE_"))
}
