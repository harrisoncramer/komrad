// Package mcp exposes the panel over MCP. It serves two surfaces: read-only SQL
// over the agent store, so an orchestrator can ask its own questions without a
// bespoke RPC for each one, and the session RPCs that carry a tool annotation,
// which is how a model spawns or changes an agent rather than only reading about
// one.
package mcp

import (
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHandlerParams configures the MCP HTTP handler. DB is the read-only handle
// the query tools use. BaseURL is where this process serves its own Connect
// endpoint, which the RPC-backed tools call back into.
type NewHandlerParams struct {
	DB      *pgxpool.Pool
	BaseURL string
}

// NewHandler builds the MCP server and returns an HTTP handler for it, to mount
// at a route such as /mcp. It exposes read-only SQL over the database alongside
// the annotated RPCs, which can write.
//
// There is no auth on it. The server listens on loopback, and the reads were
// already available to anyone who can open the database file. The writes were
// not. A caller here can spawn and delete agents, so binding the server to a
// non-loopback address would be a real change in exposure.
func NewHandler(params NewHandlerParams) (http.Handler, error) {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "claude-agents-db",
		Title:   "Claude Agents Database",
		Version: "0.1.0",
	}, &mcpsdk.ServerOptions{
		Instructions: "Access to claude-agents, a tmux panel that runs background Claude agents. " +
			"Two ways in. list_tables, describe_table, and query read the panel's Postgres database " +
			"directly, which is best for aggregates and joins; the session tools read and change " +
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
