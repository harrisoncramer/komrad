package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"weave/config"
	"weave/internal/caller"
	"weave/internal/logging"
	"weave/internal/store"
	"weave/internal/tmux"
	"weave/lib/xslice"

	"weave/internal/actions"
	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"
)

var ErrAgentNotFound = errors.New("agent not found")
var ErrWorkflowNotFound = errors.New("workflow not found")
var ErrBadDirectory = errors.New("unusable working directory")
var ErrTooManyAgents = errors.New("too many agents")
var ErrAgentArchived = errors.New("agent is archived")
var ErrSpawnerUnknown = errors.New("the spawning agent is not one this panel knows")

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

type agentFinder interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

type agentSlots interface {
	LockSlots(ctx context.Context) error
	CountLive(ctx context.Context) (int64, error)
}

type agentManager interface {
	agentFinder
	GetInWorkflow(ctx context.Context, workflowID, name string) (*agentstore.Agent, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*agentstore.Agent, error)
	ReleaseIdempotencyKey(ctx context.Context, key string) (bool, error)
	ListByID(ctx context.Context, ids []string) ([]agentstore.Agent, error)
	Create(ctx context.Context, agent agentstore.Agent) error
	List(ctx context.Context) ([]agentstore.Agent, error)
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
	CountLive(ctx context.Context) (int64, error)
	Update(ctx context.Context, agent agentstore.Agent) error
	UpdateIfLive(ctx context.Context, agent agentstore.Agent) (bool, error)
	SetStateByID(ctx context.Context, id, state string) error
	MarkSettingUpIfLive(ctx context.Context, id string) (bool, error)
	SetInterruptedAtByID(ctx context.Context, id string, at *time.Time) error
	Archive(ctx context.Context, ids []string) ([]agentstore.AgentPane, error)
	ArchiveIfUnchanged(ctx context.Context, params agentstore.ArchiveIfUnchangedParams) (bool, error)
	Restore(ctx context.Context, ids []string) error
	ActiveNameExists(ctx context.Context, workflowID, name string) (bool, error)
	LockSlots(ctx context.Context) error
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

type paneKiller interface {
	GetPanes() (map[string]tmux.PaneStatus, error)
	KillPane(pane string)
}

type paneController interface {
	paneKiller
	NewWindow(session, name, dir string) (string, error)
	SetPaneOwner(pane, agentID string) error
	GetPaneOwner(pane string) string
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

type lifecycleActioner interface {
	RunSetupAction(ctx context.Context, agent agentstore.Agent, name, pane string) (string, error)
	RunResumeAction(ctx context.Context, agent agentstore.Agent, name string) (string, error)
	RunCleanupAction(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error
}

type systemMailSender interface {
	SendSystemMail(ctx context.Context, toAgentID, body string) (*mailstore.Mail, error)
}

// workflowEmptyDeleter permanently removes a workflow left with nothing archived inside it, and
// is wired in after construction because the service implementing it takes this one as its own
// agent deleter.
type workflowEmptyDeleter interface {
	DeleteIfEmpty(ctx context.Context, workflowID string) error
}

type AgentService struct {
	tmux               paneController
	agents             agentManager
	workflows          workflowReader
	emptyWorkflows     workflowEmptyDeleter
	tx                 txRunner
	launcher           *Launcher
	harnesses          harnessGetter
	lifecycle          lifecycleActioner
	userDefaultHarness string
	userDefaultFlags   map[string]string
	maxAgents          int
	fallbackResumeRoot string
	notifier           systemMailSender
	logsDir            string
	pullRequests       pullRequestResolver
}

type NewAgentServiceParams struct {
	Tmux               paneController
	Agents             agentManager
	Workflows          workflowReader
	Tx                 txRunner
	Launcher           *Launcher
	Harnesses          harnessGetter
	Lifecycle          lifecycleActioner
	UserDefaultHarness string
	UserDefaultFlags   map[string]string
	MaxAgents          int
	FallbackResumeRoot string
	Notifier           systemMailSender
	LogsDir            string
}

// NewAgentService builds the service from its dependencies.
func NewAgentService(p NewAgentServiceParams) *AgentService {
	if p.FallbackResumeRoot == "" {
		p.FallbackResumeRoot = DefaultFallbackResumeRoot()
	}

	return &AgentService{
		tmux:               p.Tmux,
		agents:             p.Agents,
		workflows:          p.Workflows,
		tx:                 p.Tx,
		launcher:           p.Launcher,
		harnesses:          p.Harnesses,
		lifecycle:          p.Lifecycle,
		userDefaultHarness: p.UserDefaultHarness,
		userDefaultFlags:   p.UserDefaultFlags,
		maxAgents:          p.MaxAgents,
		fallbackResumeRoot: p.FallbackResumeRoot,
		notifier:           p.Notifier,
		logsDir:            p.LogsDir,
	}
}

// WithWorkflowEmptyDeleter arms the check that runs whenever a delete leaves a workflow with
// nothing archived inside it: from then on, such a workflow is removed right along with the
// agent that emptied it out, sync or async, rather than sitting there until something else
// notices.
func (s *AgentService) WithWorkflowEmptyDeleter(d workflowEmptyDeleter) *AgentService {
	s.emptyWorkflows = d

	return s
}

// IsAgentRunning reports whether the agent still owns its recorded pane.
func (s *AgentService) IsAgentRunning(agent agentstore.Agent) bool {
	return isPaneOwnedBy(s.tmux.GetPaneOwner(agent.Pane), agent.ID)
}

// GetRunningAgents returns agent liveness keyed by agent ID.
func (s *AgentService) GetRunningAgents(ctx context.Context, agents []agentstore.Agent) map[string]bool {
	return getRunningAgents(ctx, s.tmux, agents)
}

func getRunningAgents(ctx context.Context, tmux paneKiller, agents []agentstore.Agent) map[string]bool {
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

type NewAgentParams struct {
	Client         string
	Title          string
	Dir            string
	Flags          string
	SetupAction    *string
	CleanupAction  *string
	Prompt         string
	Harness        string
	WorkflowID     string
	ScheduleID     *string
	Hidden         bool
	IdempotencyKey string
	SetupPane      string
	AdoptedBranch  string
}

// NewAgent creates and launches a background agent, answering a key already spent on a launch
// with that agent and ErrAlreadyLaunched rather than a bare nil.
func (s *AgentService) NewAgent(ctx context.Context, p NewAgentParams) (*agentstore.Agent, error) {
	if p.IdempotencyKey != "" {
		existing, err := s.agents.GetByIdempotencyKey(ctx, p.IdempotencyKey)
		switch {
		case err == nil:
			// The row is written before the window is opened, so a create that failed leaves the
			// key spent on an agent that never ran and a retry answered with a corpse.
			released, err := s.releaseFailedLaunch(ctx, *existing, p.IdempotencyKey)
			if err != nil {
				return nil, err
			}
			if !released {
				return existing, ErrAlreadyLaunched
			}
		case !errors.Is(err, store.ErrNotFound):
			return nil, err
		}
	}

	var spawner *agentstore.Agent
	if id := caller.From(ctx).AgentID; id != "" {
		found, err := s.findSpawner(ctx, id)
		if err != nil {
			return nil, err
		}
		spawner = found
	}

	if err := requireRoomForAgents(ctx, s.agents, s.maxAgents, 1); err != nil {
		return nil, err
	}

	return s.launchAgent(ctx, p, spawner)
}

// releaseFailedLaunch frees an idempotency key held by a create that failed, so the next
// attempt launches instead of being answered with the corpse.
func (s *AgentService) releaseFailedLaunch(ctx context.Context, existing agentstore.Agent, key string) (bool, error) {
	// A failed cleanup belongs to an agent that ran, so only a failed create launched nothing.
	if existing.State != agentstore.StateFailedToCreate {
		return false, nil
	}

	// The row is left where it stands rather than relaunched into, since its id names the
	// worktree and branch the cleanup action is still tearing down in the background.
	return s.agents.ReleaseIdempotencyKey(ctx, key)
}

func (s *AgentService) findSpawner(ctx context.Context, id string) (*agentstore.Agent, error) {
	spawner, err := s.agents.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrSpawnerUnknown, id)
	}
	if err != nil {
		return nil, err
	}

	return spawner, nil
}

// DeleteAgents stops agents and removes those with nothing left to tear down, holding back any
// whose cleanup action is still running until it finishes.
func (s *AgentService) DeleteAgents(ctx context.Context, ids []string) error {
	agents, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return err
	}

	// A cleanup action must not run against a directory a CLI is still working in.
	killOwnedPanes(ctx, s.tmux, agents)

	bg := context.WithoutCancel(ctx)

	var immediate []agentstore.Agent
	for _, agent := range agents {
		name := s.getCleanupAction(ctx, agent)
		if s.lifecycle == nil || name == "" {
			immediate = append(immediate, agent)

			continue
		}

		agent := agent
		// The row is never dropped before the action says whether it worked.
		s.runCleanup(ctx, agent, name, func(cleanupErr error) {
			if cleanupErr != nil {
				s.failCleanup(bg, agent, cleanupErr)

				return
			}

			if err := s.removeAgents(bg, []string{agent.ID}); err != nil {
				logging.From(bg).LogAttrs(bg, slog.LevelError, "failed to remove an agent whose cleanup finished",
					append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

				return
			}

			s.cleanupEmptyWorkflows(bg, []agentstore.Agent{agent})
			notifyParents(bg, s.notifier, []agentstore.Agent{agent}, "deleted")
		})
	}

	if len(immediate) == 0 {
		return nil
	}

	if err := s.removeAgents(ctx, agentIDs(immediate)); err != nil {
		return err
	}

	s.cleanupEmptyWorkflows(ctx, immediate)
	notifyParents(ctx, s.notifier, immediate, "deleted")

	return nil
}

// cleanupEmptyWorkflows deletes each workflow among removed's that now holds nothing archived
// inside it, logging a failure rather than returning it since the agents are already gone.
func (s *AgentService) cleanupEmptyWorkflows(ctx context.Context, removed []agentstore.Agent) {
	if s.emptyWorkflows == nil {
		return
	}

	touched := xslice.UniqueBy(removed, func(agent agentstore.Agent) string { return agent.WorkflowID })
	for _, agent := range touched {
		if err := s.emptyWorkflows.DeleteIfEmpty(ctx, agent.WorkflowID); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to delete a workflow a delete left with nothing in it",
				append([]slog.Attr{slog.String(logging.WorkflowID, agent.WorkflowID)}, logging.Err(err)...)...)
		}
	}
}

// ArchiveAgents stops agents while preserving their records and workspaces.
func (s *AgentService) ArchiveAgents(ctx context.Context, ids []string) error {
	agents, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return err
	}

	// Killing what was read first keeps a failed write from hiding a live CLI, leaving the row
	// for the reconciler to tidy.
	killOwnedPanes(ctx, s.tmux, agents)

	taken, err := s.agents.Archive(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to archive %d agents: %w", len(ids), err)
	}
	// A pane recorded in between only reaches here through what the write took off the rows.
	killPanes(ctx, s.tmux, taken)

	notifyParents(ctx, s.notifier, agents, "archived")

	return nil
}

// ArchiveUnchangedAgents archives the agents whose rows still read as the caller observed them
// and returns the ones it archived, so an agent resumed since the snapshot is skipped.
func (s *AgentService) ArchiveUnchangedAgents(ctx context.Context, observed []agentstore.Agent) ([]agentstore.Agent, error) {
	var failures []error
	var archived []agentstore.Agent

	for _, agent := range observed {
		applied, err := s.agents.ArchiveIfUnchanged(ctx, agentstore.ArchiveIfUnchangedParams{ID: agent.ID, UpdatedAt: agent.UpdatedAt})
		if err != nil {
			failures = append(failures, err)

			continue
		}
		if applied {
			archived = append(archived, agent)
		}
	}

	killOwnedPanes(ctx, s.tmux, archived)
	notifyParents(ctx, s.notifier, archived, "archived")

	return archived, errors.Join(failures...)
}

func notifyParents(ctx context.Context, notifier systemMailSender, agents []agentstore.Agent, reason string) {
	actor := caller.From(ctx).AgentID

	for _, agent := range agents {
		if agent.SpawnedByAgentID == nil || *agent.SpawnedByAgentID == actor {
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

// RestoreAgents returns archived agents and their workflows to live listings.
func (s *AgentService) RestoreAgents(ctx context.Context, ids []string) ([]agentstore.Agent, error) {
	agents, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return nil, err
	}

	archived := xslice.Filter(agents, func(agent agentstore.Agent) bool { return agent.ArchivedAt != nil })
	if len(archived) == 0 {
		return agents, nil
	}

	err = s.tx.InTx(ctx, func(db store.DBTX) error {
		agentStore, workflowStore := agentstore.New(db), workflowstore.New(db)

		if err := claimRoomForAgents(ctx, agentStore, s.maxAgents, len(archived)); err != nil {
			return err
		}
		if err := requireNamesFree(ctx, agentStore, archived); err != nil {
			return err
		}
		if err := agentStore.Restore(ctx, agentIDs(archived)); err != nil {
			return err
		}
		if err := stampRestoredPRs(ctx, agentStore, archived); err != nil {
			return err
		}

		for _, id := range workflowIDs(archived) {
			if err := workflowStore.Restore(ctx, id); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to restore %d agents: %w", len(archived), err)
	}

	return s.agents.ListByID(ctx, ids)
}

func (s *AgentService) requireEveryAgent(ctx context.Context, ids []string) ([]agentstore.Agent, error) {
	agents, err := s.agents.ListByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to look up %d agents: %w", len(ids), err)
	}
	if len(agents) == len(ids) {
		return agents, nil
	}

	found := xslice.KeyBy(agents, func(agent agentstore.Agent) string { return agent.ID })

	missing := xslice.Filter(ids, func(id string) bool {
		_, ok := found[id]

		return !ok
	})

	return nil, fmt.Errorf("%w: %s", ErrAgentNotFound, describeMissing(missing))
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

// ResumeAgent relaunches a dormant agent in its previous conversation.
func (s *AgentService) ResumeAgent(ctx context.Context, id string) (*agentstore.Agent, error) {
	agent, err := s.getByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if agent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q is archived, restore it first", ErrAgentArchived, agent.Name)
	}

	if s.IsAgentRunning(*agent) {
		return agent, nil
	}

	if err := s.resume(ctx, *agent); err != nil {
		return nil, err
	}

	return s.getByID(ctx, id)
}

func isPaneOwnedBy(owner, agentID string) bool {
	return agentID != "" && owner == agentID
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

func (s *AgentService) getByID(ctx context.Context, id string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (s *AgentService) removeAgents(ctx context.Context, ids []string) error {
	// The delete is what says which pane each row really held, so a pane written after the
	// caller read the rows is reached here and nowhere else.
	var taken []agentstore.AgentPane
	if err := s.tx.InTx(ctx, func(db store.DBTX) error {
		deleted, err := agentstore.New(db).Delete(ctx, ids)
		if err != nil {
			return err
		}
		taken = deleted

		return transcriptstore.New(db).Delete(ctx, ids)
	}); err != nil {
		return err
	}

	killPanes(ctx, s.tmux, taken)
	s.removeActionLogs(ctx, ids)

	return nil
}

// removeActionLogs drops the setup and teardown logs an agent leaves on disk once its row is
// gone, logging a failure rather than returning it since a leftover file costs nothing.
func (s *AgentService) removeActionLogs(ctx context.Context, ids []string) {
	if s.logsDir == "" {
		return
	}

	for _, id := range ids {
		for _, kind := range []string{config.ActionLogSetup, config.ActionLogTeardown} {
			path := config.ActionLogPath(s.logsDir, id, kind)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				logging.From(ctx).Warn("failed to remove an agent's action log", "path", path, "agent_id", id, "error", err)
			}
		}
	}
}

func (s *AgentService) runResumeSetup(ctx context.Context, agent agentstore.Agent) (string, error) {
	name := s.getSetupAction(ctx, agent)
	if s.lifecycle == nil || name == "" {
		return "", nil
	}

	return s.lifecycle.RunResumeAction(ctx, agent, name)
}

// runCleanup starts a cleanup action, calling done exactly once with the outcome, including a
// refusal to start.
func (s *AgentService) runCleanup(ctx context.Context, agent agentstore.Agent, name string, done func(error)) {
	if s.lifecycle == nil || name == "" {
		if done != nil {
			done(nil)
		}

		return
	}

	if err := s.lifecycle.RunCleanupAction(ctx, agent, name, done); err != nil {
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

func (s *AgentService) getCleanupAction(ctx context.Context, agent agentstore.Agent) string {
	if agent.CleanupAction != nil {
		return *agent.CleanupAction
	}

	wf := s.getWorkflowFor(ctx, agent)
	if wf == nil {
		return ""
	}

	return wf.CleanupAction
}

func (s *AgentService) getSetupAction(ctx context.Context, agent agentstore.Agent) string {
	if agent.SetupAction != nil {
		return *agent.SetupAction
	}

	wf := s.getWorkflowFor(ctx, agent)
	if wf == nil {
		return ""
	}

	return wf.SetupAction
}

func (s *AgentService) getWorkflowFor(ctx context.Context, agent agentstore.Agent) *workflowstore.Workflow {
	wf, err := s.workflows.GetIncludingArchived(ctx, agent.WorkflowID)
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

func (s *AgentService) resume(ctx context.Context, agent agentstore.Agent) error {
	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return err
	}

	// Until this write the row reads as a dormant agent nobody is touching, which is exactly what
	// a background pass archives.
	live, err := s.agents.MarkSettingUpIfLive(ctx, agent.ID)
	if err != nil {
		return fmt.Errorf("failed to mark %q as resuming: %w", agent.Name, err)
	}
	if !live {
		return fmt.Errorf("%w: %q was archived while it was being resumed, restore it first", ErrAgentArchived, agent.Name)
	}

	// Read before the resume starts overwriting the copy it was handed, since the restore
	// below runs after those writes and is putting back what the resume found.
	prior := agent.State

	var pane string
	launched := false
	defer func() {
		if launched {
			return
		}

		// Nothing else in weave moves a row out of setting_up or takes away a window a resume
		// opened, so both are put back here rather than depending on the caller still being
		// around to see it happen.
		restore, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
		defer cancel()

		if pane != "" {
			s.tmux.KillPane(pane)
		}

		if err := s.agents.SetStateByID(restore, agent.ID, prior); err != nil {
			logging.From(restore).LogAttrs(restore, slog.LevelError, "failed to put back the state of an agent whose resume gave up",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
		}
	}()

	named, err := s.runResumeSetup(ctx, agent)
	if err != nil {
		return err
	}

	dir, err := namedDir(named)
	if err != nil {
		return err
	}

	if dir == "" {
		dir = s.getResumeDir(ctx, agent, hn)
	}

	pane, err = s.tmux.NewWindow(ManagerSession, agent.Name, dir)
	if err != nil {
		return fmt.Errorf("failed to open a window for %q: %w", agent.Name, err)
	}
	if err := s.tmux.SetPaneOwner(pane, agent.ID); err != nil {
		return fmt.Errorf("failed to stamp the pane for %q: %w", agent.Name, err)
	}

	// A tmux server killed whole takes every pane and the reconciler's view of them at once, so
	// the interrupted turn is stamped here rather than left to the reconciler.
	if agent.InterruptedAt == nil && turnUnfinished(agent.State) {
		at := time.Now()
		if err := s.agents.SetInterruptedAtByID(ctx, agent.ID, &at); err != nil {
			return fmt.Errorf("failed to record the cut-off turn for %q: %w", agent.Name, err)
		}

		agent.InterruptedAt = &at
	}

	agent.Pane = pane
	agent.State = agentstore.StateIdle
	agent.Cwd = dir
	// An archive or a delete takes the row without reading the resume mark, so this write is where
	// a resume finds out it lost and has to take away the pane it opened.
	written, err := s.agents.UpdateIfLive(ctx, agent)
	if err != nil {
		return fmt.Errorf("failed to resume agent %q: %w", agent.Name, err)
	}
	if !written {
		return fmt.Errorf("%w: %q was archived or deleted while it was being resumed", ErrAgentArchived, agent.Name)
	}

	// Codex takes no session id on the command line, so an agent may have none and its relaunch is
	// a fresh conversation with no turn to read back over.
	resuming := agent.SessionID != ""

	prompt := ""
	if resuming && agent.InterruptedAt != nil {
		prompt = interruptedPrompt
	}

	if err := s.launcher.launch(hn, launchParams{
		Pane:      pane,
		Dir:       dir,
		Flags:     agent.Flags,
		Prompt:    prompt,
		SessionID: agent.SessionID,
		AgentID:   agent.ID,
		Resume:    resuming,
	}); err != nil {
		return fmt.Errorf("failed to relaunch the agent for %q: %w", agent.Name, err)
	}

	launched = true

	// A recorded turn does not outlive the relaunch, since the conversation holding it has either
	// just been asked about it or is gone.
	if agent.InterruptedAt != nil {
		if err := s.agents.SetInterruptedAtByID(ctx, agent.ID, nil); err != nil {
			return fmt.Errorf("failed to clear the interrupted turn for %q: %w", agent.Name, err)
		}
	}

	return nil
}
