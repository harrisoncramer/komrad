// Package mcp serves komrade's API to the agents themselves, turning each RPC method into a tool
// an agent's CLI can call.
//
// The tools are generated from the same descriptors the handlers are built on, so a tool takes
// the request's fields as its inputs and its annotations say whether it only reads. That is
// what lets one agent spawn, address, or read another without komrade keeping a second
// hand-written surface in step with the first.
//
// A tool's annotation also says which roles it is offered to, and the caller names itself on
// every request, so a reviewer is served the tools for reading work and an implementer the
// tools for writing it.
package mcp

import (
	"context"
	"fmt"
	"net/http"

	annotationv1 "komrade/proto/gen/annotation/v1"

	"komrade/internal/caller"

	"github.com/jackc/pgx/v5/pgxpool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// servedRoles is the roles a server is built for.
var servedRoles = []annotationv1.Role{
	annotationv1.Role_ROLE_UNSPECIFIED,
	annotationv1.Role_ROLE_IMPLEMENTER,
	annotationv1.Role_ROLE_REVIEWER,
	annotationv1.Role_ROLE_ORCHESTRATOR,
}

// RoleOf answers what the agent was launched to do.
type RoleOf func(ctx context.Context, agentID string) annotationv1.Role

type NewHandlerParams struct {
	DB      *pgxpool.Pool
	BaseURL string
	// RoleOf places the caller, serving every tool when it is nil.
	RoleOf RoleOf
}

// NewHandler builds the MCP server as an HTTP handler with no auth on it, safe only while it
// listens on loopback.
func NewHandler(params NewHandlerParams) (http.Handler, error) {
	servers := make(map[annotationv1.Role]*mcpsdk.Server, len(servedRoles))
	for _, role := range servedRoles {
		server, err := newServer(params, role)
		if err != nil {
			return nil, fmt.Errorf("failed to build the tool server for %s: %w", role, err)
		}

		servers[role] = server
	}

	return mcpsdk.NewStreamableHTTPHandler(
		func(request *http.Request) *mcpsdk.Server {
			server, ok := servers[getCallerRole(request, params.RoleOf)]
			if !ok {
				return servers[annotationv1.Role_ROLE_UNSPECIFIED]
			}

			return server
		},
		&mcpsdk.StreamableHTTPOptions{Stateless: true},
	), nil
}

// getCallerRole reads the role of whoever is making the request.
func getCallerRole(request *http.Request, roleOf RoleOf) annotationv1.Role {
	agentID := caller.FromHeader(request.Header).AgentID
	if roleOf == nil || agentID == "" {
		return annotationv1.Role_ROLE_UNSPECIFIED
	}

	return roleOf(request.Context(), agentID)
}

func newServer(params NewHandlerParams, role annotationv1.Role) (*mcpsdk.Server, error) {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "komrade-db",
		Title:   "Claude Agents Database",
		Version: "0.1.0",
	}, &mcpsdk.ServerOptions{
		Instructions: "Access to komrade, a tmux panel that runs background coding agents. " +
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
