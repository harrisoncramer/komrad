package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"weave/internal/caller"
	"weave/internal/logging"
	"weave/internal/store"
	"weave/internal/tmux"
	"weave/lib/xslice"

	"weave/internal/actions"
	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	workflowstore "weave/internal/store/workflow"
)

var ErrAgentNotFound = errors.New("agent not found")
var ErrWorkflowNotFound = errors.New("workflow not found")
var ErrBadDirectory = errors.New("unusable working directory")
var ErrTooManyAgents = errors.New("too many agents")
var ErrAgentArchived = errors.New("agent is archived")
var ErrSpawnerUnknown = errors.New("the spawning agent is not one this panel knows")
var ErrParentNotFound = errors.New("parent agent not found")
var ErrParentIsChild = errors.New("a child agent may not have children of its own")
var ErrChildOwnsLaunch = errors.New("a child agent takes its launch from its parent")
var ErrTooManyChildren = errors.New("too many children")

// ErrChildNeedsParentDirectory says the agent a child was to be attached to has no directory
// recorded, which is the one thing a child cannot be given a substitute for.
var ErrChildNeedsParentDirectory = errors.New("a child agent needs its parent's directory to run in")

// ErrChildRidesAlong says a child was named on its own by an archive, a delete, or a restore
// that left its parent out, which is the one thing a child cannot be without it.
var ErrChildRidesAlong = errors.New("a child agent goes with its parent")

// ErrOutsideFamily says a child tried to reach an agent outside its own family, which is its
// parent and that parent's other children.
var ErrOutsideFamily = errors.New("a child agent may only mail its parent and its parent's other children")

// ErrAlreadyLaunched says an idempotency key was already spent on an agent that launched, and
// is returned alongside that agent rather than instead of it, so that a caller which has to
// know whether this call did the launching can tell.
var ErrAlreadyLaunched = errors.New("this idempotency key already launched an agent")

var (
	ErrHarnessUnknown      = harness.ErrNotRegistered
	ErrActionNotFound      = actions.ErrActionNotFound
	ErrActionNotExecutable = actions.ErrNotExecutable
	ErrActionNotStartable  = actions.ErrNotStartable
	ErrActionDirMissing    = actions.ErrDirMissing
)

type (
	Action       = actions.Action
	ActionHeader = actions.Header
)

type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

type agentCounter interface {
	CountLive(ctx context.Context) (int64, error)
}

type childCounter interface {
	CountLiveChildren(ctx context.Context, parentID string) (int, error)
}

type childSlots interface {
	childCounter
	LockSlots(ctx context.Context) error
}

// childLister reads the agents attached to a set of parents, which is what an archive, a
// delete, or a restore has to add to the ids it was given.
type childLister interface {
	ListChildren(ctx context.Context, parentIDs []string) ([]agentstore.Agent, error)
}

type agentSlots interface {
	agentCounter
	LockSlots(ctx context.Context) error
}

type nameChecker interface {
	ActiveNameExists(ctx context.Context, workflowID, name string) (bool, error)
}

// agentFailWriter records that an agent's create or cleanup gave up, which is a write to the
// row and a write to the metadata beside it.
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

// paneOwners is what tmux is asked to settle whether an agent is still running, one pane at a
// time or in a listing.
type paneOwners interface {
	paneLister
	GetPaneOwner(pane string) string
}

type paneController interface {
	paneKiller
	paneOwners
	NewWindow(session, name, dir string) (string, error)
	SetPaneOwner(pane, agentID string) error
	GetClientPath(client string) string
}

type paneWriter interface {
	SendKeys(target string, keys ...string) error
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

// agentActions is the seam onto the executables a user hangs off an agent's life, which are
// fired on the way in, on the way back in, and on the way out.
type agentActions interface {
	RunSetupAction(ctx context.Context, agent agentstore.Agent, name, pane string) (string, error)
	RunResumeAction(ctx context.Context, agent agentstore.Agent, name string) (string, error)
	RunCleanupAction(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error
}

type systemMailSender interface {
	SendSystemMail(ctx context.Context, toAgentID, body string) (*mailstore.Mail, error)
}

func isPaneOwnedBy(owner, agentID string) bool {
	return agentID != "" && owner == agentID
}

func getRunningAgents(ctx context.Context, tmux paneLister, agents []agentstore.Agent) map[string]bool {
	panes, err := tmux.GetPanes()
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to read panes, reporting every agent as not running",
			logging.Err(err)...)
	}

	running := make(map[string]bool, len(agents))
	for _, agent := range agents {
		running[agent.ID] = isPaneOwnedBy(panes[agent.Pane].Owner, agent.ID)
	}

	return running
}

func killOwnedPanes(ctx context.Context, tmux paneKiller, agents []agentstore.Agent) {
	killPanes(ctx, tmux, xslice.Map(agents, func(agent agentstore.Agent) agentstore.AgentPane {
		return agentstore.AgentPane{AgentID: agent.ID, Pane: agent.Pane}
	}))
}

// killPanes kills each pane tmux still says belongs to the agent named beside it, since a tmux
// server restart reuses pane ids and leaves every recorded pane pointing somewhere new.
func killPanes(ctx context.Context, tmux paneKiller, owned []agentstore.AgentPane) {
	panes, err := tmux.GetPanes()
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to read panes, killing none of them",
			logging.Err(err)...)

		return
	}

	for _, owner := range owned {
		if isPaneOwnedBy(panes[owner.Pane].Owner, owner.AgentID) {
			tmux.KillPane(owner.Pane)
		}
	}
}

// runCleanup starts a cleanup action, calling done exactly once with the outcome, including a
// refusal to start.
func runCleanup(ctx context.Context, cleanup agentActions, agent agentstore.Agent, name string, done func(error)) {
	if cleanup == nil || name == "" {
		if done != nil {
			done(nil)
		}

		return
	}

	if err := cleanup.RunCleanupAction(ctx, agent, name, done); err != nil {
		attrs := []slog.Attr{
			slog.String(logging.ActionName, name),
			slog.String(logging.ActionAgentID, agent.ID),
			slog.String(logging.ActionAgentCwd, agent.Cwd),
		}
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the cleanup action would not start", append(attrs, logging.Err(err)...)...)

		if done != nil {
			done(err)
		}
	}
}

func cleanupActionFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) string {
	if agent.CleanupAction != nil {
		return *agent.CleanupAction
	}

	wf := workflowFor(ctx, workflows, agent)
	if wf == nil {
		return ""
	}

	return wf.CleanupAction
}

func setupActionFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) string {
	if agent.SetupAction != nil {
		return *agent.SetupAction
	}

	wf := workflowFor(ctx, workflows, agent)
	if wf == nil {
		return ""
	}

	return wf.SetupAction
}

func workflowFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) *workflowstore.Workflow {
	wf, err := workflows.GetIncludingArchived(ctx, agent.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		attrs := []slog.Attr{
			slog.String(logging.WorkflowID, agent.WorkflowID),
			slog.String(logging.ActionAgentID, agent.ID),
			slog.String(logging.ActionAgentCwd, agent.Cwd),
		}
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the workflow behind an agent could not be read", append(attrs, logging.Err(err)...)...)

		return nil
	}

	return wf
}

const maxFailureReasonLen = 480

// failAs marks an agent as having failed create or cleanup and leaves the row where it was, so
// a delete that cannot finish leaves an archived row in the archive the user was clearing.
func failAs(ctx context.Context, agents agentFailWriter, agent agentstore.Agent, state, verb string, cause error) {
	agent.State = state
	if err := agents.Update(ctx, agent); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, fmt.Sprintf("failed to record why an agent's %s failed", verb),
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

		return
	}

	reason := failureReason(cause)
	if _, err := agents.SetMetadata(ctx, agentstore.SetMetadataParams{
		ID:    agent.ID,
		Key:   "error",
		Value: &reason,
	}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, fmt.Sprintf("failed to record why an agent's %s failed", verb),
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

func failureReason(cause error) string {
	if cause == nil {
		return ""
	}

	text := strings.Join(strings.Fields(cause.Error()), " ")
	if len(text) > maxFailureReasonLen {
		text = text[:maxFailureReasonLen]
	}

	return text
}

func notifyParents(ctx context.Context, notifier systemMailSender, agents []agentstore.Agent, reason string) {
	actor := caller.From(ctx).AgentID

	for _, agent := range agents {
		if agent.SpawnedByAgentID == nil || *agent.SpawnedByAgentID == actor {
			continue
		}
		// A child whose spawner is the parent it is attached to only ever goes as part of that
		// parent going, so the mail would be addressed to an agent already on its way out.
		if agent.ParentAgentID != nil && *agent.SpawnedByAgentID == *agent.ParentAgentID {
			continue
		}

		body := fmt.Sprintf("Your delegate %q (%s) was %s.", agent.Name, agent.ID, reason)
		if _, err := notifier.SendSystemMail(ctx, *agent.SpawnedByAgentID, body); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "could not tell an agent's spawner it was gone",
				append([]slog.Attr{
					slog.String(logging.DelegateAgentID, agent.ID),
					slog.String(logging.TargetAgentID, *agent.SpawnedByAgentID),
					slog.String(logging.LifecycleReason, reason),
				}, logging.Err(err)...)...)
		}
	}
}

const namedMissing = 5

func describeMissing(missing []string) string {
	if len(missing) <= namedMissing {
		return strings.Join(missing, ", ")
	}

	return fmt.Sprintf("%s, and %d more", strings.Join(missing[:namedMissing], ", "), len(missing)-namedMissing)
}

func agentIDs(agents []agentstore.Agent) []string {
	return xslice.Map(agents, func(agent agentstore.Agent) string { return agent.ID })
}

func workflowIDs(agents []agentstore.Agent) []string {
	unique := xslice.UniqueBy(agents, func(agent agentstore.Agent) string { return agent.WorkflowID })

	return xslice.Map(unique, func(agent agentstore.Agent) string { return agent.WorkflowID })
}
