// Package caller carries who is making a request, and the id that ties its log lines together,
// between the panel, the CLI, an agent's MCP call and the server.
//
// The identity travels in headers between processes and on the context within one, so a handler
// and the store under it read it without taking it as an argument. [Transport] copies it onto
// outgoing requests. Log lines use it to name the agent that asked. A request that names nobody
// is unattributed, and its log lines leave the fields out.
package caller
