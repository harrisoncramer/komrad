// Package internaljobs holds each kind of background job komrad queues and the worker that does
// it: routing an event to its mail, launching a schedule's agent, waking an agent so it reads its
// mail, archiving a reviewer once its approval is released, deleting agents and worktrees, and
// fetching a check's log.
package internaljobs
