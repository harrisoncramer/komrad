package ui

import (
	"context"

	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeHooks struct {
	hookv1connect.HookServiceClient
}

func (f *fakeHooks) ListHooks(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[hookv1.ListHooksResponse], error) {
	return connect.NewResponse(&hookv1.ListHooksResponse{Hooks: []*hookv1.Hook{
		{Name: "install-deps", Executable: true},
		{Name: "unrunnable"},
		{Name: "copy-env", Executable: true},
	}}), nil
}
