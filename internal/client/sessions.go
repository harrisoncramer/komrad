// Package client holds the small pieces every caller of the service needs, so
// the panel, the CLI, and anything else added later read the API the same way
// rather than each writing its own paging loop.
package client

import (
	"context"
	"fmt"

	paginationv1 "claude-agents/proto/gen/pagination/v1"
	sessionv1 "claude-agents/proto/gen/session/v1"
	"claude-agents/proto/gen/session/v1/sessionv1connect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

// A caller wanting every chat asks for large pages rather than a first page: a
// list this size is one or two round trips. The cap on pages is a stop for a
// token the server somehow never retires, so a bug there cannot spin forever.
const (
	pageSize = 200
	maxPages = 50
)

// ListAllSessions returns every chat, following the page tokens until the
// listing is exhausted.
func ListAllSessions(ctx context.Context, sessions sessionv1connect.SessionServiceClient) ([]*sessionv1.Session, error) {
	var (
		all    []*sessionv1.Session
		cursor string
	)

	for range maxPages {
		page := &paginationv1.Pagination{Limit: proto.Int32(pageSize)}
		if cursor != "" {
			page.Cursor = proto.String(cursor)
		}

		resp, err := sessions.ListSessions(ctx, connect.NewRequest(&sessionv1.ListSessionsRequest{Pagination: page}))
		if err != nil {
			return nil, fmt.Errorf("failed to list sessions: %w", err)
		}

		all = append(all, resp.Msg.GetSessions()...)

		cursor = resp.Msg.GetNextPageToken()
		if cursor == "" {
			return all, nil
		}
	}

	return all, nil
}
