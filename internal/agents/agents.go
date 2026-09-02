package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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

// ErrParentStillArchived says a restore named a child whose parent stays archived, leaving the
// child a row with nothing to be drawn under.
var ErrParentStillArchived = errors.New("a child agent cannot come back before its parent")

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
	RunProvisionAction(ctx context.Context, agent agentstore.Agent, name, pane string) (string, error)
	RunResumeAction(ctx context.Context, agent agentstore.Agent, name string) (string, error)
	RunDeprovisionAction(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error
	RunHookAction(ctx context.Context, agent agentstore.Agent, name, event string) error
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

// hookRunParams is one moment's worth of work: the hooks a workflow lists, the agent they run
// against, and the moment itself, which is what the script reads out of WEAVE_TRIGGER.
type hookRunParams struct {
	actions agentActions
	agent   agentstore.Agent
	hooks   []workflowstore.Hook
	event   string
}

// runHooks runs the hooks on one moment in order, waiting for each, and stops at the first that
// fails so the ones after it are not handed a directory half prepared.
func runHooks(ctx context.Context, p hookRunParams) error {
	if p.actions == nil {
		return nil
	}

	for _, hook := range p.hooks {
		if hook.Event != p.event {
			continue
		}

		if err := p.actions.RunHookAction(ctx, p.agent, hook.Action, p.event); err != nil {
			return err
		}
	}

	return nil
}

// startHooks runs a moment's hooks in the background, since the call that set the moment off has
// nothing to wait for: the agent is already where the hooks are about to look, and one that
// fails there is reported rather than undone.
func startHooks(ctx context.Context, p hookRunParams) {
	if p.actions == nil || !slices.ContainsFunc(p.hooks, func(hook workflowstore.Hook) bool { return hook.Event == p.event }) {
		return
	}

	bg := context.WithoutCancel(ctx)

	go func() {
		if err := runHooks(bg, hookRunParams{
			actions: p.actions,
			agent:   p.agent,
			hooks:   p.hooks,
			event:   p.event,
		}); err != nil {
			logging.From(bg).LogAttrs(bg, slog.LevelError, "a hook failed",
				append([]slog.Attr{
					slog.String(logging.ActionTrigger, p.event),
					slog.String(logging.ActionAgentID, p.agent.ID),
				}, logging.Err(err)...)...)
		}
	}()
}

// agentHooks are the hooks a workflow lists for one of its agents, which is none for an agent
// working in a directory somebody else prepared: a child in its parent's checkout, or one told
// to keep what it was given, where a hook would be writing into a tree nobody asked it to touch.
func agentHooks(ctx context.Context, workflows workflowReader, agent agentstore.Agent) []workflowstore.Hook {
	if agent.ParentAgentID != nil || isProvisionerDisabled(agent.ProvisionEnabled) {
		return nil
	}

	wf := workflowFor(ctx, workflows, agent)
	if wf == nil {
		return nil
	}

	return wf.Hooks
}

// runDeprovision starts a deprovision action, calling done exactly once with the outcome,
// including a refusal to start.
func runDeprovision(ctx context.Context, deprovision agentActions, agent agentstore.Agent, name string, done func(error)) {
	if deprovision == nil || name == "" {
		if done != nil {
			done(nil)
		}

		return
	}

	if err := deprovision.RunDeprovisionAction(ctx, agent, name, done); err != nil {
		attrs := []slog.Attr{
			slog.String(logging.ActionName, name),
			slog.String(logging.ActionAgentID, agent.ID),
			slog.String(logging.ActionAgentCwd, agent.Cwd),
		}
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the deprovision action would not start", append(attrs, logging.Err(err)...)...)

		if done != nil {
			done(err)
		}
	}
}

// isProvisionerDisabled reports whether an agent has been told to keep the directory it was
// given rather than be provisioned one, which is what adopting a branch a human has open, a
// child in its parent's checkout, and a reviewer reading somebody else's tree each mean.
func isProvisionerDisabled(override *bool) bool {
	return override != nil && !*override
}

// provisionsWorktree reports whether an agent's directory is weave's to make, which is its
// workflow's worktree toggle unless the agent itself has been told to keep what it was given.
func provisionsWorktree(ctx context.Context, workflows workflowReader, agent agentstore.Agent) bool {
	if isProvisionerDisabled(agent.ProvisionEnabled) {
		return false
	}

	wf := workflowFor(ctx, workflows, agent)

	return wf != nil && wf.Worktree.Enabled
}

func deprovisionActionFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) string {
	if !provisionsWorktree(ctx, workflows, agent) {
		return ""
	}

	return worktreeDeprovisionAction
}

func provisionActionFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) string {
	if !provisionsWorktree(ctx, workflows, agent) {
		return ""
	}

	return worktreeProvisionAction
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

// notifyParents tells the spawner of each agent in told that it is gone, where going is every
// agent the call is taking rather than only the ones being told about.
func notifyParents(ctx context.Context, notifier systemMailSender, told, going []agentstore.Agent, reason string) {
	actor := caller.From(ctx).AgentID

	leaving := make(map[string]bool, len(going))
	for _, agent := range going {
		leaving[agent.ID] = true
	}

	for _, agent := range told {
		if agent.SpawnedByAgentID == nil || *agent.SpawnedByAgentID == actor {
			continue
		}
		// A spawner going in the same call has nobody left to read the mail, which is what
		// happens to a parent that takes its own children with it.
		if leaving[*agent.SpawnedByAgentID] {
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
