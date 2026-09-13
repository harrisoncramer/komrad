package agents

import (
	"weave/internal/fail"

	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"weave/internal/logging"
	"weave/internal/store"
	"weave/internal/tmux"
	"weave/lib/xslice"

	"weave/internal/harness"
	"weave/internal/scripts"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	workflowstore "weave/internal/store/workflow"
)

var (
	ErrAgentNotFound    = fail.NotFound("agent not found")
	ErrWorkflowNotFound = fail.NotFound("workflow not found")
	ErrBadDirectory     = fail.InvalidArgument("unusable working directory")
	ErrTooManyAgents    = fail.Exhausted("too many agents")
	ErrAgentArchived    = fail.Precondition("agent is archived")
	ErrSpawnerUnknown   = fail.InvalidArgument("the spawning agent is not one this panel knows")
	ErrParentNotFound   = fail.NotFound("parent agent not found")
	ErrParentIsChild    = fail.InvalidArgument("a child agent may not have children of its own")
	ErrChildOwnsLaunch  = fail.InvalidArgument("a child agent takes its launch from its parent")
	ErrTooManyChildren  = fail.Exhausted("too many children")

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

	// ErrOutsideFamily says a child tried to reach an agent other than its parent or one of that
	// parent's other children.
	ErrOutsideFamily = fail.Denied("a child agent may only mail its parent and its parent's other children")

	// ErrAlreadyLaunched says an idempotency key was already spent on an agent that launched, and
	// is returned alongside that agent rather than instead of it.
	ErrAlreadyLaunched     = fail.AlreadyExists("this idempotency key already launched an agent")
	ErrHarnessUnknown      = harness.ErrNotRegistered
	ErrScriptNotFound      = scripts.ErrScriptNotFound
	ErrScriptNotExecutable = scripts.ErrNotExecutable
	ErrScriptNotStartable  = scripts.ErrNotStartable
	ErrScriptDirMissing    = scripts.ErrDirMissing
)

type (
	Script       = scripts.Script
	ScriptHeader = scripts.Header
)

type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

type nameChecker interface {
	ActiveNameExists(ctx context.Context, workflowID, name string) (bool, error)
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

// killPanes kills each pane tmux still says belongs to the agent named beside it.
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
// against, and the moment itself.
type hookRunParams struct {
	scripts agentScriptRunner
	agent   agentstore.Agent
	hooks   []workflowstore.Hook
	event   string
}

// runHooks runs the hooks on one moment in order, waiting for each and stopping at the first
// that fails.
func runHooks(ctx context.Context, params hookRunParams) error {
	if params.scripts == nil {
		return nil
	}

	for _, hook := range params.hooks {
		if hook.Event != params.event {
			continue
		}

		if err := params.scripts.RunHook(ctx, params.agent, hook.Script, params.event); err != nil {
			return err
		}
	}

	return nil
}

// hasHookOn reports whether a workflow hangs any script on one moment.
func hasHookOn(hooks []workflowstore.Hook, event string) bool {
	return slices.ContainsFunc(hooks, func(hook workflowstore.Hook) bool { return hook.Event == event })
}

// startHooks runs a moment's hooks in the background.
func startHooks(ctx context.Context, params hookRunParams) {
	if params.scripts == nil || !hasHookOn(params.hooks, params.event) {
		return
	}

	bg := context.WithoutCancel(ctx)

	go func() {
		if err := runHooks(bg, hookRunParams{
			scripts: params.scripts,
			agent:   params.agent,
			hooks:   params.hooks,
			event:   params.event,
		}); err != nil {
			logging.From(bg).LogAttrs(bg, slog.LevelError, "a hook failed",
				append([]slog.Attr{
					slog.String(logging.RunTrigger, params.event),
					slog.String(logging.ScriptAgentID, params.agent.ID),
				}, logging.Err(err)...)...)
		}
	}()
}

// startRestoreHooks tells every agent a restore took out of the archive that its row is back
// but dormant.
func startRestoreHooks(ctx context.Context, scripts agentScriptRunner, workflows workflowReader, restored []agentstore.Agent) {
	for _, agent := range restored {
		startHooks(ctx, hookRunParams{
			scripts: scripts,
			agent:   agent,
			hooks:   agentHooks(ctx, workflows, agent),
			event:   workflowstore.HookPostRestore,
		})
	}
}

// agentHooks are the hooks a workflow lists for one of its agents, which is none for an agent
// working in a directory somebody else prepared.
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

// runDeprovision starts a deprovisioner, calling done exactly once with the outcome,
// including a refusal to start.
func runDeprovision(ctx context.Context, deprovision agentScriptRunner, agent agentstore.Agent, name string, done func(error)) {
	if deprovision == nil || name == "" {
		if done != nil {
			done(nil)
		}

		return
	}

	if err := deprovision.RunDeprovision(ctx, agent, name, done); err != nil {
		attrs := []slog.Attr{
			slog.String(logging.ScriptName, name),
			slog.String(logging.ScriptAgentID, agent.ID),
			slog.String(logging.ScriptAgentCwd, agent.Cwd),
		}
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the deprovisioner would not start", append(attrs, logging.Err(err)...)...)

		if done != nil {
			done(err)
		}
	}
}

// isProvisionerDisabled reports whether an agent has been told to keep the directory it was
// given rather than be provisioned one.
func isProvisionerDisabled(override *bool) bool {
	return override != nil && !*override
}

// doesProvisionWorktree reports whether an agent's directory is weave's to make, which is its
// workflow's worktree toggle unless the agent itself has been told to keep what it was given.
func doesProvisionWorktree(ctx context.Context, workflows workflowReader, agent agentstore.Agent) bool {
	if isProvisionerDisabled(agent.ProvisionEnabled) {
		return false
	}

	wf := workflowFor(ctx, workflows, agent)

	return wf != nil && wf.Worktree.Enabled
}

func deprovisionerFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) string {
	if !doesProvisionWorktree(ctx, workflows, agent) {
		return ""
	}

	return worktreeDeprovisioner
}

func provisionerFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) string {
	if !doesProvisionWorktree(ctx, workflows, agent) {
		return ""
	}

	return worktreeProvisioner
}

func workflowFor(ctx context.Context, workflows workflowReader, agent agentstore.Agent) *workflowstore.Workflow {
	wf, err := workflows.GetIncludingArchived(ctx, agent.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		attrs := []slog.Attr{
			slog.String(logging.WorkflowID, agent.WorkflowID),
			slog.String(logging.ScriptAgentID, agent.ID),
			slog.String(logging.ScriptAgentCwd, agent.Cwd),
		}
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the workflow behind an agent could not be read", append(attrs, logging.Err(err)...)...)

		return nil
	}

	return wf
}

const maxFailureReasonLen = 480

// failAs marks an agent as having failed create or cleanup and leaves the row where it was.
func failAs(ctx context.Context, agents agentFailWriter, agent agentstore.Agent, state agentstore.State, verb string, cause error) {
	agent.State = state
	if err := agents.Update(ctx, agent); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record why an agent's lifecycle action failed",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.LifecycleAction, verb),
			}, logging.Err(err)...)...)

		return
	}

	reason := failureReason(cause)
	if _, err := agents.SetMetadata(ctx, agentstore.SetMetadataParams{
		ID:    agent.ID,
		Key:   "error",
		Value: &reason,
	}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record why an agent's lifecycle action failed",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.LifecycleAction, verb),
			}, logging.Err(err)...)...)
	}
}

// failureReason is a cause as a row can hold it, cut and scrubbed of what Postgres refuses in a
// text column.
func failureReason(cause error) string {
	if cause == nil {
		return ""
	}

	text := strings.ToValidUTF8(strings.ReplaceAll(cause.Error(), "\x00", ""), "")
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > maxFailureReasonLen {
		text = strings.ToValidUTF8(text[:maxFailureReasonLen], "")
	}

	return text
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
