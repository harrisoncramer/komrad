package client

import (
	"context"

	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	paginationv1 "weave/proto/gen/pagination/v1"
	viewv1 "weave/proto/gen/view/v1"

	"connectrpc.com/connect"
)

// ListAllAgents returns every live agent, following the page tokens until the
// listing is exhausted.
func ListAllAgents(ctx context.Context, agents agentv1connect.AgentServiceClient) ([]*agentv1.Agent, error) {
	return listAllAgents(ctx, agents, false)
}

// ListAllArchivedAgents returns every archived agent, the two listings never mixing.
func ListAllArchivedAgents(ctx context.Context, agents agentv1connect.AgentServiceClient) ([]*agentv1.Agent, error) {
	return listAllAgents(ctx, agents, true)
}

func listAllAgents(ctx context.Context, agents agentv1connect.AgentServiceClient, archived bool) ([]*agentv1.Agent, error) {
	return listAll(ctx, func(ctx context.Context, page *paginationv1.Pagination) ([]*agentv1.Agent, string, error) {
		resp, err := agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{
			Pagination: page,
			Archived:   archived,
			View:       viewv1.View_VIEW_FULL,
		}))
		if err != nil {
			return nil, "", err
		}

		return resp.Msg.GetAgents(), resp.Msg.GetNextPageToken(), nil
	})
}
