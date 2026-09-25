package agents

import (
	"context"

	"komrade/internal/fail"
	"komrade/internal/harness"
	"komrade/internal/scriptrun"
	"komrade/internal/scripts"
	"komrade/internal/store"
	"komrade/internal/tmux"

	agentstore "komrade/internal/store/agent"
	workflowstore "komrade/internal/store/workflow"
)

var (
	ErrAgentNotFound        = fail.NotFound("agent not found")
	ErrWorkflowNotFound     = fail.NotFound("workflow not found")
	ErrBadDirectory         = scripts.ErrBadDirectory
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

	// ErrAlreadyLaunched says an idempotency key was already spent on an agent that launched, and
	// is returned alongside that agent rather than instead of it.
	ErrAlreadyLaunched      = fail.AlreadyExists("this idempotency key already launched an agent")
	ErrHarnessUnknown       = harness.ErrNotRegistered
	ErrScriptNotFound       = scripts.ErrScriptNotFound
	ErrScriptNotExecutable  = scripts.ErrNotExecutable
	ErrScriptNotStartable   = scripts.ErrNotStartable
	ErrScriptDirMissing     = scripts.ErrDirMissing
	ErrProvisionFailed      = scriptrun.ErrProvisionFailed
	ErrHookFailed           = scriptrun.ErrHookFailed
	ErrScriptAlreadyRunning = scriptrun.ErrScriptAlreadyRunning
)

type (
	Script       = scripts.Script
	ScriptHeader = scripts.Header
)

type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

type nameChecker interface {
	ExistsActiveName(ctx context.Context, workflowID, name string) (bool, error)
}

// agentFailWriter records that an agent's create or cleanup gave up.
type agentFailWriter interface {
	Update(ctx context.Context, agent agentstore.Agent) error
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

type paneLister interface {
	GetPanes() (map[string]tmux.PaneStatus, error)
}

type paneKiller interface {
	paneLister
	KillPane(pane string)
}

// paneOwnerReader asks tmux who owns a pane, one at a time or in a listing.
type paneOwnerReader interface {
	paneLister
	GetPaneOwner(pane string) string
}

type paneController interface {
	paneKiller
	paneOwnerReader
	NewWindow(session, name, dir string) (string, error)
	SetPaneOwner(pane, agentID string) error
	GetHostPath() string
}

// paneStarter runs an agent's CLI as the pane's own process.
type paneStarter interface {
	RespawnPane(pane, dir string, argv ...string) error
	GetDefaultShell() string
}

type workflowReader interface {
	Get(ctx context.Context, id string) (*workflowstore.Workflow, error)
	GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error)
}

type txRunner interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

type harnessGetter interface {
	GetByID(id string) (harness.Harness, error)
}

// buildTarget names an agent the way a channel or an observer reaches it.
func buildTarget(agent agentstore.Agent) harness.Target {
	return harness.Target{
		AgentID:   agent.ID,
		Pane:      agent.Pane,
		HarnessID: agent.Harness,
	}
}

// agentScriptRunner is the seam onto the executables a user hangs off an agent's life.
type agentScriptRunner interface {
	RunProvision(ctx context.Context, agent agentstore.Agent, name string) (string, error)
	RunResume(ctx context.Context, agent agentstore.Agent, name string) (string, error)
	RunDeprovision(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error
	RunHook(ctx context.Context, agent agentstore.Agent, name, event string) error
}
