// Package internaljobs holds each kind of background job komrad queues and the worker that does
// it. The jobs route an event to its mail, launch a schedule's agent, wake an agent so it reads its
// mail, archive a reviewer once its approval is released, delete agents and worktrees, and fetch a
// check's log.
package internaljobs
