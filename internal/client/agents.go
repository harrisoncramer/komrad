// Package client holds the small pieces every caller of the service needs, so the
// panel and the CLI read the API the same way rather than each writing its own
// paging loop.
package client

import (
	"context"

	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	paginationv1 "weave/proto/gen/pagination/v1"

	"connectrpc.com/connect"
)

// ListAllAgents returns every live agent, following the page tokens until the
// listing is exhausted.
func ListAllAgents(ctx context.Context, agents agentv1connect.AgentServiceClient) ([]*agentv1.Agent, error) {
	return listAllAgents(ctx, agents, false)
}

// ListAllDeletedAgents returns every soft-deleted agent, which is the trash. The
// two listings never mix, so this is a call of its own rather than a flag the
// caller has to remember to read back off the rows.
func ListAllDeletedAgents(ctx context.Context, agents agentv1connect.AgentServiceClient) ([]*agentv1.Agent, error) {
	return listAllAgents(ctx, agents, true)
}

func listAllAgents(ctx context.Context, agents agentv1connect.AgentServiceClient, deleted bool) ([]*agentv1.Agent, error) {
	return listAll(ctx, func(ctx context.Context, page *paginationv1.Pagination) ([]*agentv1.Agent, string, error) {
		resp, err := agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{
			Pagination: page,
			Deleted:    deleted,
		}))
		if err != nil {
			return nil, "", err
		}

		return resp.Msg.GetAgents(), resp.Msg.GetNextPageToken(), nil
	})
}
