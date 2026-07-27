// Package client holds the small pieces every caller of the service needs, so the
// panel and the CLI read the API the same way rather than each writing its own
// paging loop.
package client

import (
	"context"
	"fmt"

	agentv1 "claude-agents/proto/gen/agent/v1"
	"claude-agents/proto/gen/agent/v1/agentv1connect"
	paginationv1 "claude-agents/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

// A caller wanting every agent asks for large pages rather than a first page,
// since a list this size is one or two round trips. The cap on pages is a stop for
// a token the server never retires, so a bug there cannot spin forever.
const (
	pageSize = 200
	maxPages = 50
)

// ListAllAgents returns every agent, following the page tokens until the listing
// is exhausted.
func ListAllAgents(ctx context.Context, agents agentv1connect.AgentServiceClient) ([]*agentv1.Agent, error) {
	var (
		all    []*agentv1.Agent
		cursor string
	)

	for range maxPages {
		page := &paginationv1.Pagination{Limit: proto.Int32(pageSize)}
		if cursor != "" {
			page.Cursor = proto.String(cursor)
		}

		resp, err := agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{Pagination: page}))
		if err != nil {
			return nil, fmt.Errorf("failed to list agents: %w", err)
		}

		all = append(all, resp.Msg.GetAgents()...)

		cursor = resp.Msg.GetNextPageToken()
		if cursor == "" {
			return all, nil
		}
	}

	return all, nil
}
