// Package mcp serves weave's API to the agents themselves, turning each RPC method into a tool
// an agent's CLI can call.
//
// The tools are generated from the same descriptors the handlers are built on, so a tool takes
// the request's fields as its inputs and its annotations say whether it only reads. That is
// what lets one agent spawn, address, or read another without weave keeping a second
// hand-written surface in step with the first.
//
// A tool's annotation also says which roles it is offered to, and the caller names itself on
// every request, so a reviewer is served the tools for reading work and an implementer the
// tools for writing it rather than both being handed the whole surface to carry.
package mcp

import (
	"context"
	"fmt"
	"net/http"

	annotationv1 "weave/proto/gen/annotation/v1"

	"weave/internal/caller"

	"github.com/jackc/pgx/v5/pgxpool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// servedRoles is the roles a server is built for, one of which every caller is served.
var servedRoles = []annotationv1.Role{
	annotationv1.Role_ROLE_UNSPECIFIED,
	annotationv1.Role_ROLE_IMPLEMENTER,
	annotationv1.Role_ROLE_REVIEWER,
	annotationv1.Role_ROLE_ORCHESTRATOR,
}

// RoleOf answers what the agent was launched to do, ROLE_UNSPECIFIED for one weave cannot place.
type RoleOf func(ctx context.Context, agentID string) annotationv1.Role

type NewHandlerParams struct {
	DB      *pgxpool.Pool
	BaseURL string
	// RoleOf places the caller so it is served that role's tools, every tool when it is nil.
	RoleOf RoleOf
}

// NewHandler builds the MCP server as an HTTP handler with no auth on it, safe only while it
// listens on loopback.
func NewHandler(params NewHandlerParams) (http.Handler, error) {
	servers := make(map[annotationv1.Role]*mcpsdk.Server, len(servedRoles))
	for _, role := range servedRoles {
		server, err := newServer(params, role)
		if err != nil {
			return nil, err
		}
		servers[role] = server
	}

	return mcpsdk.NewStreamableHTTPHandler(
		func(request *http.Request) *mcpsdk.Server {
			server, ok := servers[callerRole(request, params.RoleOf)]
			if !ok {
				return servers[annotationv1.Role_ROLE_UNSPECIFIED]
			}

			return server
		},
		&mcpsdk.StreamableHTTPOptions{Stateless: true},
	), nil
}

// callerRole places whoever is making the request, unspecified for one naming no agent.
func callerRole(request *http.Request, roleOf RoleOf) annotationv1.Role {
	agentID := caller.FromHeader(request.Header).AgentID
	if roleOf == nil || agentID == "" {
		return annotationv1.Role_ROLE_UNSPECIFIED
	}

	return roleOf(request.Context(), agentID)
}

func newServer(params NewHandlerParams, role annotationv1.Role) (*mcpsdk.Server, error) {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "weave-db",
		Title:   "Claude Agents Database",
		Version: "0.1.0",
	}, &mcpsdk.ServerOptions{
		Instructions: "Access to weave, a tmux panel that runs background coding agents. " +
			"Two ways in. list_tables, describe_table, and query read the panel's Postgres database " +
			"directly, which is best for aggregates and joins. The agent tools read and change " +
			"the agents themselves, and are the only way to spawn, rename, or remove one.",
	})

	tools := &dbTools{db: params.DB}
	tools.registerTools(server)

	if err := registerRPCTools(server, params.BaseURL, role); err != nil {
		return nil, fmt.Errorf("failed to register rpc tools: %w", err)
	}

	return server, nil
}
