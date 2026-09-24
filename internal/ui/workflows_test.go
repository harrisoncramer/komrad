package ui

import (
	"context"

	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

func describeRows(rows []agentRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.header {
			out = append(out, "header:"+row.workflow.Name)
			continue
		}

		out = append(out, "agent:"+row.item.Agent.Name)
	}

	return out
}

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
