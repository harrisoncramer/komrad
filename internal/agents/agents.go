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
	ListByID(ctx context.Context, ids []string) ([]agentstore.Agent, error)
	Create(ctx context.Context, agent agentstore.Agent) error
	List(ctx context.Context) ([]agentstore.Agent, error)
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
	CountLive(ctx context.Context) (int64, error)
	Update(ctx context.Context, agent agentstore.Agent) error
	SetInterruptedAtByID(ctx context.Context, id string, at *time.Time) error
	Archive(ctx context.Context, ids []string) error
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

// workflowEmptyDeleter permanently removes a workflow left with nothing archived inside it.
// It is wired in after construction, through WithWorkflowEmptyDeleter, because the service
// that implements it takes this one as its own agent deleter — a plain constructor argument
// here would make the two depend on each other before either exists.
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

// NewAgent creates and launches a background agent.
func (s *AgentService) NewAgent(ctx context.Context, p NewAgentParams) (*agentstore.Agent, error) {
	if p.IdempotencyKey != "" {
		existing, err := s.agents.GetByIdempotencyKey(ctx, p.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}

	spawner, err := s.findSpawner(ctx)
	if err != nil {
		return nil, err
	}

	if err := requireRoomForAgents(ctx, s.agents, s.maxAgents, 1); err != nil {
		return nil, err
	}

	return s.launchAgent(ctx, p, spawner)
}

func (s *AgentService) findSpawner(ctx context.Context) (*agentstore.Agent, error) {
	id := caller.From(ctx).AgentID
	if id == "" {
		return nil, nil
	}

	spawner, err := s.agents.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if spawner == nil {
		return nil, fmt.Errorf("%w: %q", ErrSpawnerUnknown, id)
	}

	return spawner, nil
}

// DeleteAgents stops agents and removes those with nothing left to tear down. An agent whose
// cleanup action is still running is left in place until that action finishes: only then, in
// the background, does it either get removed (the action succeeded) or get marked
// failed_to_cleanup (it did not) — the row is never dropped ahead of knowing which.
func (s *AgentService) DeleteAgents(ctx context.Context, ids []string) error {
	agents, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return err
	}

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

// cleanupEmptyWorkflows deletes whatever workflow among removed's each now holds nothing
// archived inside it, once per distinct workflow rather than once per agent. A failure is
// logged rather than returned, the same as a failed notification: the agents are already
// gone, so there is nothing left for the caller to undo.
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

	killOwnedPanes(ctx, s.tmux, agents)

	if err := s.agents.Archive(ctx, ids); err != nil {
		return fmt.Errorf("failed to archive %d agents: %w", len(ids), err)
	}

	notifyParents(ctx, s.notifier, agents, "archived")

	return nil
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
	running := getRunningAgents(ctx, tmux, agents)
	for _, agent := range agents {
		if running[agent.ID] {
			tmux.KillPane(agent.Pane)
		}
	}
}

func (s *AgentService) getByID(ctx context.Context, id string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, ErrAgentNotFound
	}

	return agent, nil
}

func (s *AgentService) removeAgents(ctx context.Context, ids []string) error {
	if err := s.tx.InTx(ctx, func(db store.DBTX) error {
		if err := agentstore.New(db).Delete(ctx, ids); err != nil {
			return err
		}

		return transcriptstore.New(db).Delete(ctx, ids)
	}); err != nil {
		return err
	}

	s.removeActionLogs(ctx, ids)

	return nil
}

// removeActionLogs drops the durable setup/teardown logs an agent leaves on disk, once its
// row is really gone. Best-effort: a leftover file costs nothing an agent removed a second
// time won't also clean up, so a failure here is logged rather than turned into an error the
// caller has to do anything about.
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

// runCleanup starts a cleanup action. done, when given, is called exactly once with the
// action's outcome: nil if it is not configured at all, or once RunCleanupAction's own done
// callback reports how the run went — including a refusal to even start, which is reported
// here rather than left for the caller to notice only via the log.
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
	// A resume only reaches an agent whose pane is already gone, so a row that still says
	// thinking is one whose turn went with that pane. Those are the same two facts a
	// reconcile pass goes on, and a restart kills the tmux server before any pass can read
	// a pane, so this is the only place left to notice.
	cutOff := agent.InterruptedAt != nil || agent.State == agentstore.StateThinking

	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return err
	}

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

	pane, err := s.tmux.NewWindow(ManagerSession, agent.Name, dir)
	if err != nil {
		return fmt.Errorf("failed to open a window for %q: %w", agent.Name, err)
	}
	if err := s.tmux.SetPaneOwner(pane, agent.ID); err != nil {
		s.tmux.KillPane(pane)

		return fmt.Errorf("failed to stamp the pane for %q: %w", agent.Name, err)
	}

	// A resume only gets this far for an agent whose pane has gone, so a turn the store still
	// has running went with it. The reconciler stamps that when it watches a pane go, but a
	// tmux server killed whole takes every pane and the reconciler's view of them at once, so
	// the stamp is written here too rather than the state being read instead of it. The write
	// comes before the state is overwritten, so a relaunch that fails leaves the turn to be
	// asked about by the resume after it.
	if agent.InterruptedAt == nil && turnUnfinished(agent.State) {
		at := time.Now()
		if err := s.agents.SetInterruptedAtByID(ctx, agent.ID, &at); err != nil {
			s.tmux.KillPane(pane)

			return fmt.Errorf("failed to record the cut-off turn for %q: %w", agent.Name, err)
		}

		agent.InterruptedAt = &at
	}

	agent.Pane = pane
	agent.State = agentstore.StateIdle
	agent.Cwd = dir
	if err := s.agents.Update(ctx, agent); err != nil {
		s.tmux.KillPane(pane)

		return fmt.Errorf("failed to resume agent %q: %w", agent.Name, err)
	}

	// A stored conversation is what a resume reopens, and an agent may have none: Codex
	// takes no session id on the command line, so its id is only learned later from its
	// rollout file, and never at all if that file is not matched. Such a relaunch is a
	// fresh conversation, which cannot be asked to read back over a turn it never held.
	resuming := agent.SessionID != ""

	prompt := ""
	if resuming && cutOff {
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

	// A recorded turn does not outlive the relaunch. Either the conversation holding it has
	// just been asked about it, or that conversation is gone and no later resume reaches it.
	if agent.InterruptedAt != nil {
		if err := s.agents.SetInterruptedAtByID(ctx, agent.ID, nil); err != nil {
			return fmt.Errorf("failed to clear the interrupted turn for %q: %w", agent.Name, err)
		}
	}

	return nil
}
