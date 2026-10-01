// Package agenterr declares the sentinels the agent lifecycle returns.
package agenterr

import "komrad/internal/fail"

var (
	ErrAgentNotFound        = fail.NotFound("agent not found")
	ErrWorkflowNotFound     = fail.NotFound("workflow not found")
	ErrTooManyAgents        = fail.Exhausted("too many agents")
	ErrAgentArchived        = fail.Precondition("agent is archived")
	ErrSpawnerUnknown       = fail.InvalidArgument("the spawning agent is not one this panel knows")
	ErrOutsideWorkflow      = fail.Denied("an orchestrator spawns only into its own workflow")
	ErrParentNotFound       = fail.NotFound("parent agent not found")
	ErrParentIsChild        = fail.InvalidArgument("a child agent may not have children of its own")
	ErrParentIsOrchestrator = fail.InvalidArgument("an orchestrator may not have children of its own")
	ErrChildOwnsLaunch      = fail.InvalidArgument("a child agent takes its launch from its parent")
	ErrTooManyChildren      = fail.Exhausted("too many children")
	ErrTooManySpawned       = fail.Exhausted("too many spawned agents")
	ErrChildDeleting        = fail.Precondition("a child is still being deleted by another delete")
	ErrTeardownFailed       = fail.Internal("an agent's teardown failed")

	// ErrChildNeedsParentDirectory says the agent a child was to be attached to has no directory
	// recorded.
	ErrChildNeedsParentDirectory = fail.Precondition("a child agent needs its parent's directory to run in")

	// ErrParentStillArchived says a restore named a child whose parent stays archived.
	ErrParentStillArchived = fail.Precondition("a child agent cannot come back before its parent")

	// ErrResumeInFlight says another resume of the same agent is already under way.
	ErrResumeInFlight = fail.Precondition("this agent is already being resumed")

	// ErrReviewerAlreadyRunning says the agent asking already has a reviewer of its own reading
	// its work.
	ErrReviewerAlreadyRunning = fail.AlreadyExists("this agent already has a reviewer")

	// ErrNoLiveReviewer says the agent has no reviewer still owed an answer.
	ErrNoLiveReviewer = fail.Precondition("this agent has no reviewer still reading its work")

	// ErrAlreadyLaunched says an idempotency key was already spent on an agent that launched.
	ErrAlreadyLaunched = fail.AlreadyExists("this idempotency key already launched an agent")

	// ErrAgentHeld is anything else komrad was asked to do to an agent a person is holding.
	ErrAgentHeld = fail.Precondition("this agent is held")

	// ErrReviewNeedsOwnWork says the agent asked for has no work of its own to be read.
	ErrReviewNeedsOwnWork = fail.Precondition("this agent has no work of its own to have read")

	// ErrUnsettled is an agent that could not be seen to come to rest.
	ErrUnsettled = fail.Precondition("the agent did not stop")
)
