// Package mcp exposes the agent store over MCP as a read-only SQL surface, so
// an orchestrator can explore the schema and ask its own questions of the chats
// and workflows rather than needing a bespoke RPC per question.
package mcp

import (
	"database/sql"
	"net/http"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHandlerParams configures the MCP HTTP handler. DB is the read-only handle
// the tools query through.
type NewHandlerParams struct {
	DB *sql.DB
}

// NewHandler builds the read-only database MCP server and returns an HTTP
// handler for it. Mount it at a route such as /mcp. There is no auth on it: the
// server listens on loopback, and everything it exposes is already readable by
// anyone who can open the database file. Binding the server to a non-loopback
// address would change that.
func NewHandler(params NewHandlerParams) http.Handler {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "claude-agents-db",
		Title:   "Claude Agents Database",
		Version: "0.1.0",
	}, &mcpsdk.ServerOptions{
		Instructions: "Read-only access to the claude-agents SQLite database of background " +
			"Claude chats and the workflows they belong to. Use list_tables and describe_table " +
			"to explore the schema, then query to run SELECT statements.",
	})

	tools := &dbTools{db: params.DB}
	tools.registerTools(server)

	return mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return server },
		&mcpsdk.StreamableHTTPOptions{Stateless: true},
	)
}
