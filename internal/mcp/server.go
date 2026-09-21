// Package mcp serves weave's API to the agents themselves, turning each RPC method into a tool
// an agent's CLI can call.
//
// The tools are generated from the same descriptors the handlers are built on, so a tool takes
// the request's fields as its inputs and its annotations say whether it only reads. That is
// what lets one agent spawn, address, or read another without weave keeping a second
// hand-written surface in step with the first.
package mcp

import (
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type NewHandlerParams struct {
	DB      *pgxpool.Pool
	BaseURL string
}

// NewHandler builds the MCP server as an HTTP handler with no auth on it, safe only while it
// listens on loopback.
func NewHandler(params NewHandlerParams) (http.Handler, error) {
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

	if err := registerRPCTools(server, params.BaseURL); err != nil {
		return nil, fmt.Errorf("failed to register rpc tools: %w", err)
	}

	return mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return server },
		&mcpsdk.StreamableHTTPOptions{Stateless: true},
	), nil
}
