// Package agenterr declares the sentinels the agent lifecycle returns.
package agenterr

import "komrad/internal/fail"

var (
	ErrAgentNotFound      = fail.NotFound("agent not found")
	ErrWorkflowNotFound   = fail.NotFound("workflow not found")
	ErrTooManyAgents      = fail.Exhausted("too many agents")
	ErrAgentArchived      = fail.Precondition("agent is archived")
	ErrSpawnerUnknown     = fail.InvalidArgument("the spawning agent is not one this panel knows")
	ErrOutsideWorkflow    = fail.Denied("an orchestrator spawns only into its own workflow")
	ErrReviewerOwnsLaunch = fail.InvalidArgument("a reviewer takes its launch from its task")
	ErrTooManyReviewers   = fail.Exhausted("too many reviewers")
	ErrTooManyTasks       = fail.Exhausted("too many tasks")
	ErrReviewerDeleting   = fail.Precondition("a reviewer is still being deleted by another delete")
	ErrTeardownFailed     = fail.Internal("an agent's teardown failed")

	// ErrReviewerNeedsDirectory says the task a reviewer was launched onto has no directory recorded.
	ErrReviewerNeedsDirectory = fail.Precondition("a reviewer needs its task's directory to run in")

	// ErrWriterGoesWithTask says a call named a task's writer on its own, which only ever moves
	// with its task.
	ErrWriterGoesWithTask = fail.Precondition("a task's writer is archived and deleted with its task")

	// ErrTaskArchived says a restore named a reviewer whose task is still archived.
	ErrTaskArchived = fail.Precondition("the agent's task is archived")

	// ErrResumeInFlight says another resume of the same agent is already under way.
	ErrResumeInFlight = fail.Precondition("this agent is already being resumed")

	// ErrAlreadyLaunched says an idempotency key was already spent on an agent that launched.
	ErrAlreadyLaunched = fail.AlreadyExists("this idempotency key already launched an agent")

	// ErrReviewNeedsOwnWork says the agent asked for has no work of its own to be read.
	ErrReviewNeedsOwnWork = fail.Precondition("this agent has no work of its own to have read")

	// ErrDeleteByAgent is an agent deleting a task or a workflow.
	ErrDeleteByAgent = fail.Denied("only a person deletes tasks and workflows")
)
