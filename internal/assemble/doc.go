// Package assemble builds the agent and task rows that the panel and the agent listing both
// show, from the resources a caller holds.
//
// A [World] holds those resources by key and applies each watch message to them. [AgentRows]
// and [TaskRows] then join agents with their unread mail, their task's pull requests and the
// local review of its work, so the panel and the server compute a row the same way.
package assemble
