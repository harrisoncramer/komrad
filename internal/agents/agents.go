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

// The refusals that come from the adapters underneath, restated here because a caller maps
// what the service returns and should not have to reach past it to name a failure.
var (
	ErrHarnessUnknown      = harness.ErrNotRegistered
	ErrActionNotFound      = actions.ErrActionNotFound
	ErrActionNotExecutable = actions.ErrNotExecutable
	ErrActionNotStartable  = actions.ErrNotStartable
	ErrActionDirMissing    = actions.ErrDirMissing
)

// Action is one of the user's actions as the service reports it, and ActionHeader is what
// the comments at the top of its file declare. They are the adapter's types under another
// name, so a caller describes an action without depending on what runs one.
type (
	Action       = actions.Action
	ActionHeader = actions.Header
)

// agentFinder is the one-method view of the agent store shared by every collaborator
// that only ever needs to look an agent up by id.
type agentFinder interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

// agentSlots is the room-for-one-more view of the agent store: take the lock that
// serialises launches against each other, and count what is already running.
type agentSlots interface {
	LockSlots(ctx context.Context) error
	CountLive(ctx context.Context) (int64, error)
}

// agentManager is the agent store as the service and its launch machinery use it,
// whether held on AgentService itself or opened fresh inside a transaction.
type agentManager interface {
	agentFinder
	GetInWorkflow(ctx context.Context, workflowID, name string) (*agentstore.Agent, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*agentstore.Agent, error)
	ListByID(ctx context.Context, ids []string) ([]agentstore.Agent, error)
	Create(ctx context.Context, agent agentstore.Agent) error
	CountLive(ctx context.Context) (int64, error)
	Update(ctx context.Context, agent agentstore.Agent) error
	Archive(ctx context.Context, ids []string) error
	ActiveNameExists(ctx context.Context, workflowID, name string) (bool, error)
	LockSlots(ctx context.Context) error
}

// paneKiller is the tmux surface a workflow archive needs: which agents are still
// running, and stopping the ones that are.
type paneKiller interface {
	GetPaneOwners() (map[string]string, error)
	KillPane(pane string)
}

// paneController is the tmux surface AgentService needs to launch and resume an
// agent: paneKiller, plus opening a window and stamping who owns it.
type paneController interface {
	paneKiller
	NewWindow(session, name, dir string) (string, error)
	SetPaneOwner(pane, agentID string) error
	GetPaneOwner(pane string) string
	GetClientPath(client string) string
}

// paneWriter is how a launch types into an agent's pane. The command is
// not executed directly: it is typed into a pane and parsed by the shell running
// there, which is why the harness that assembles it quotes everything interpolated.
type paneWriter interface {
	SendKeys(target string, keys ...string) error
}

// workflowReader is the workflow store as the services over it read it: a live workflow to
// launch into, and an archived one's blueprint to fall back on its setup and cleanup.
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
	RunCleanupAction(ctx context.Context, agent agentstore.Agent, name string) error
}

// parentNotifier tells a spawning agent its delegate is gone. It is SendSystemMail
// rather than the general SendMail so nothing outside this package, including the RPC
// surface that builds a SendMailParams from a request on the wire, can speak as weave.
type parentNotifier interface {
	SendSystemMail(ctx context.Context, toAgentID, body string) (*mailstore.Mail, error)
}

// AgentService manages agent records, panes, and lifecycle actions.
type AgentService struct {
	tmux               paneController
	agents             agentManager
	workflows          workflowReader
	tx                 txRunner
	launcher           *Launcher
	harnesses          harnessGetter
	lifecycle          lifecycleActioner
	userDefaultHarness string
	userDefaultFlags   map[string]string
	maxAgents          int
	fallbackResumeRoot string
	notifier           parentNotifier
}

// NewAgentServiceParams carries the agent service's dependencies.
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
	Notifier           parentNotifier
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
	}
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
	owners, err := tmux.GetPaneOwners()
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to read pane owners, reporting every agent as not running",
			logging.Err(err)...)
	}

	running := make(map[string]bool, len(agents))
	for _, agent := range agents {
		running[agent.ID] = isPaneOwnedBy(owners[agent.Pane], agent.ID)
	}

	return running
}

// NewAgentParams configures a background agent to create.
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

// findSpawner is the agent named on the request's caller header, or nil for a create the
// panel itself made. Its id is recorded as the new agent's parent and its name is what the
// delegate briefing calls it, so an id that names no agent this panel knows fails the
// create rather than recording a reference nothing can resolve.
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

// DeleteAgents stops and permanently removes agents by ID.
func (s *AgentService) DeleteAgents(ctx context.Context, ids []string) error {
	agents, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return err
	}

	killOwnedPanes(ctx, s.tmux, agents)
	for _, agent := range agents {
		s.runCleanup(ctx, agent, s.getCleanupAction(ctx, agent))
	}

	if err := s.removeAgents(ctx, ids); err != nil {
		return err
	}

	notifyParents(ctx, s.notifier, agents, "deleted")

	return nil
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

// notifyParents tells each agent's spawner it is gone. Failures are logged rather than
// returned, since a bad mailbox shouldn't block deletion. An actor removing its own delegate
// is skipped, since it already knows what it just did.
func notifyParents(ctx context.Context, notifier parentNotifier, agents []agentstore.Agent, reason string) {
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

	// Non-archived agents already hold their slot, so counting them again would overcount room needed.
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

// Destructive batches are all or nothing.
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

// Pane IDs can be reused, so kill only panes still stamped with their agent ID.
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

// Agent rows and transcripts must be removed in one transaction.
func (s *AgentService) removeAgents(ctx context.Context, ids []string) error {
	return s.tx.InTx(ctx, func(db store.DBTX) error {
		if err := agentstore.New(db).Delete(ctx, ids); err != nil {
			return err
		}

		return transcriptstore.New(db).Delete(ctx, ids)
	})
}

func (s *AgentService) runResumeSetup(ctx context.Context, agent agentstore.Agent) (string, error) {
	name := s.getSetupAction(ctx, agent)
	if s.lifecycle == nil || name == "" {
		return "", nil
	}

	return s.lifecycle.RunResumeAction(ctx, agent, name)
}

// Cleanup failures cannot prevent an agent from being deleted.
func (s *AgentService) runCleanup(ctx context.Context, agent agentstore.Agent, name string) {
	if s.lifecycle == nil || name == "" {
		return
	}

	err := s.lifecycle.RunCleanupAction(ctx, agent, name)
	if err == nil {
		return
	}

	attrs := []slog.Attr{
		slog.String(logging.ActionName, name),
		slog.String(logging.ActionAgentID, agent.ID),
		slog.String(logging.ActionAgentCwd, agent.Cwd),
	}
	logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the cleanup action would not start", append(attrs, logging.Err(err)...)...)
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

	agent.Pane = pane
	agent.State = agentstore.StateIdle
	agent.Cwd = dir
	if err := s.agents.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to resume agent %q: %w", agent.Name, err)
	}

	if err := s.launcher.launch(hn, launchParams{
		Pane:      pane,
		Dir:       dir,
		Flags:     agent.Flags,
		SessionID: agent.SessionID,
		AgentID:   agent.ID,
		Resume:    true,
	}); err != nil {
		return fmt.Errorf("failed to relaunch the agent for %q: %w", agent.Name, err)
	}

	return nil
}
