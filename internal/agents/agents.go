package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"weave/config"
	"weave/internal/caller"
	"weave/internal/logging"
	"weave/internal/store"
	"weave/lib/xslice"
	"weave/lib/xstr"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	messagestore "weave/internal/store/message"
	"weave/internal/store/transcript"
	"weave/internal/store/workflow"
)

var ErrAgentNotFound = errors.New("agent not found")
var ErrWorkflowNotFound = errors.New("workflow not found")
var ErrBadDirectory = errors.New("unusable working directory")
var ErrTooManyAgents = errors.New("too many agents")
var ErrAgentArchived = errors.New("agent is archived")
var ErrSpawnerUnknown = errors.New("the spawning agent is not one this panel knows")

type agentManager interface {
	GetInWorkflow(ctx context.Context, workflowID, name string) (*agentstore.Agent, error)
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*agentstore.Agent, error)
	ListByID(ctx context.Context, ids []string) ([]agentstore.Agent, error)
	Create(ctx context.Context, agent agentstore.Agent) error
	CountLive(ctx context.Context) (int64, error)
	Update(ctx context.Context, agent agentstore.Agent) error
	Archive(ctx context.Context, ids []string) error
	ActiveNameExists(ctx context.Context, workflowID, name string) (bool, error)
}

type paneOwnerGetter interface {
	GetPaneOwners() (map[string]string, error)
}

type paneKiller interface {
	paneOwnerGetter
	KillPane(pane string)
}

type paneController interface {
	paneKiller
	NewWindow(session, name, dir string) (string, error)
	SetPaneOwner(pane, agentID string) error
	GetPaneOwner(pane string) string
	GetClientPath(client string) string
}

type liveWorkflowReader interface {
	Get(ctx context.Context, id string) (*workflow.Workflow, error)
}

type storedWorkflowReader interface {
	GetIncludingArchived(ctx context.Context, id string) (*workflow.Workflow, error)
}

type workflowReader interface {
	liveWorkflowReader
	storedWorkflowReader
}

type txRunner interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

type harnessGetter interface {
	GetByID(id string) (harness.Harness, error)
}

type lifecycleActioner interface {
	RunSetupAction(ctx context.Context, agent agentstore.Agent, name string) (string, error)
	RunResumeAction(ctx context.Context, agent agentstore.Agent, name string) (string, error)
	RunCleanupAction(ctx context.Context, agent agentstore.Agent, name string) error
}

type agentCounter interface {
	CountLive(ctx context.Context) (int64, error)
}

type agentClaimer interface {
	agentCounter
	LockSlots(ctx context.Context) error
}

type agentNamer interface {
	ActiveNameExists(ctx context.Context, workflowID, name string) (bool, error)
}

// parentNotifier tells a spawning agent its delegate is gone. It is SendSystemMessage
// rather than the general SendMessage so nothing outside this package, including the RPC
// surface that builds a SendMessageParams from a request on the wire, can speak as weave.
type parentNotifier interface {
	SendSystemMessage(ctx context.Context, toAgentID, body string) (*messagestore.Message, error)
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

func getRunningAgents(ctx context.Context, tmux paneOwnerGetter, agents []agentstore.Agent) map[string]bool {
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

	plan, err := s.planLaunch(ctx, p)
	if err != nil {
		return nil, err
	}

	// The pane must be stamped with the agent ID before the reconciler can see it.
	id, err := store.NewID()
	if err != nil {
		return nil, err
	}

	launch := newAgentLaunch(p, plan, id, plan.harness.MintSessionID(), spawner)

	if err := s.runLaunchSetup(ctx, launch); err != nil {
		return nil, err
	}

	// Setup may create resources before any agent row exists.
	launched := false
	defer func() {
		if !launched {
			s.unwindLaunch(ctx, launch)
		}
	}()

	if err := s.openLaunchPane(launch); err != nil {
		return nil, err
	}

	// Claim the slot with the insert because setup cannot run inside the transaction.
	created, err := s.createLaunchRow(ctx, launch)
	if err != nil {
		return nil, err
	}

	if err := s.startLaunch(launch); err != nil {
		if deleteErr := s.removeLaunchRow(ctx, launch); deleteErr != nil {
			return nil, fmt.Errorf("failed to launch the agent for %q (%w), and failed to clean up: %w", plan.name, err, deleteErr)
		}

		return nil, fmt.Errorf("failed to launch the agent for %q: %w", plan.name, err)
	}

	launched = true

	return created, nil
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

// agentLaunch tracks the state and progress of a single agent being created,
// so a failure partway through unwinds only the pane and setup action that
// actually ran instead of everything.
type agentLaunch struct {
	params    NewAgentParams
	plan      *launchPlan
	id        string
	sessionID string
	spawner   *agentstore.Agent
	agent     agentstore.Agent
	setupName string
	setupRan  bool
	pane      string
}

func newAgentLaunch(params NewAgentParams, plan *launchPlan, id, sessionID string, spawner *agentstore.Agent) *agentLaunch {
	return &agentLaunch{
		params:    params,
		plan:      plan,
		id:        id,
		sessionID: sessionID,
		spawner:   spawner,
		agent:     plan.agent(params, id, sessionID, plan.dir, "", spawner),
	}
}

// runLaunchSetup runs the workflow's setup action, if any, against the planned agent.
func (s *AgentService) runLaunchSetup(ctx context.Context, l *agentLaunch) error {
	named, err := s.runSetup(ctx, l.agent, l.plan.setupAction)
	if err != nil {
		return err
	}

	l.setupName = named
	l.setupRan = true

	return nil
}

// openLaunchPane resolves the working directory, opens the tmux window, and
// stamps it with the agent's ID.
func (s *AgentService) openLaunchPane(l *agentLaunch) error {
	dir, err := s.setupDir(l.plan.dir, l.setupName)
	if err != nil {
		return err
	}

	l.agent = l.plan.agent(l.params, l.id, l.sessionID, dir, "", l.spawner)

	pane, err := s.tmux.NewWindow(ManagerSession, l.plan.name, dir)
	if err != nil {
		return fmt.Errorf("failed to open a window for %q: %w", l.plan.name, err)
	}
	l.pane = pane
	l.agent = l.plan.agent(l.params, l.id, l.sessionID, dir, pane, l.spawner)

	if err := s.tmux.SetPaneOwner(pane, l.id); err != nil {
		return fmt.Errorf("failed to stamp the pane for %q: %w", l.plan.name, err)
	}

	return nil
}

// createLaunchRow claims a capacity slot and inserts the agent record in one
// transaction, then reads the row back.
func (s *AgentService) createLaunchRow(ctx context.Context, l *agentLaunch) (*agentstore.Agent, error) {
	if err := s.createUnderCeiling(ctx, l.agent); err != nil {
		return nil, err
	}

	created, err := s.agents.GetInWorkflow(ctx, l.params.WorkflowID, l.plan.name)
	if err != nil {
		return nil, fmt.Errorf("failed to read back agent %q: %w", l.plan.name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("agent %q vanished after being recorded", l.plan.name)
	}

	return created, nil
}

// startLaunch hands the pane to the harness launcher.
func (s *AgentService) startLaunch(l *agentLaunch) error {
	params := launchParams{
		Pane:            l.pane,
		Dir:             l.agent.Cwd,
		Flags:           l.plan.flags,
		Prompt:          l.params.Prompt,
		WorkflowContext: l.plan.workflowContext,
		SessionID:       l.sessionID,
		AgentID:         l.id,
	}
	if l.spawner != nil {
		params.SpawnedByAgentID = l.spawner.ID
		params.SpawnedByName = l.spawner.Name
	}

	return s.launcher.launch(l.plan.harness, params)
}

// unwindLaunch reverses whatever of the pane and setup action actually ran.
// The agent row is removed separately, by removeLaunchRow, only once
// launching has actually failed.
func (s *AgentService) unwindLaunch(ctx context.Context, l *agentLaunch) {
	if l.pane != "" {
		s.tmux.KillPane(l.pane)
	}
	if l.setupRan {
		s.runCleanup(ctx, l.agent, l.plan.cleanupAction)
	}
}

func (s *AgentService) removeLaunchRow(ctx context.Context, l *agentLaunch) error {
	return s.removeAgents(ctx, []string{l.id})
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

// notifyParents tells each agent's spawner it is gone, one message per delegate that had
// one. A person or a tool asked for these agents to go away; a mailbox that refuses the
// news, or a parent that went away in the same batch and no longer accepts mail, is not a
// reason to undo that, so failures are logged rather than returned.
func notifyParents(ctx context.Context, notifier parentNotifier, agents []agentstore.Agent, reason string) {
	for _, agent := range agents {
		if agent.SpawnedByAgentID == nil {
			continue
		}

		body := fmt.Sprintf("Your delegate %q (%s) was %s.", agent.Name, agent.ID, reason)
		if _, err := notifier.SendSystemMessage(ctx, *agent.SpawnedByAgentID, body); err != nil {
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

	// Agents that are not archived are already where a restore would put them, and asking
	// for room for them would refuse a batch over the slots they already hold.
	archived := xslice.Filter(agents, func(agent agentstore.Agent) bool { return agent.ArchivedAt != nil })
	if len(archived) == 0 {
		return agents, nil
	}

	err = s.tx.InTx(ctx, func(db store.DBTX) error {
		agentStore, workflowStore := agentstore.New(db), workflow.New(db)

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

	found := make(map[string]bool, len(agents))
	for _, agent := range agents {
		found[agent.ID] = true
	}

	missing := xslice.Filter(ids, func(id string) bool { return !found[id] })

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
	seen := make(map[string]bool, len(agents))

	var ids []string
	for _, agent := range agents {
		if seen[agent.WorkflowID] {
			continue
		}
		seen[agent.WorkflowID] = true
		ids = append(ids, agent.WorkflowID)
	}

	return ids
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

		return transcript.New(db).Delete(ctx, ids)
	})
}

// Claim the capacity slot and insert the agent in one transaction.
func (s *AgentService) createUnderCeiling(ctx context.Context, agent agentstore.Agent) error {
	return s.tx.InTx(ctx, func(db store.DBTX) error {
		agentStore := agentstore.New(db)

		if err := claimRoomForAgents(ctx, agentStore, s.maxAgents, 1); err != nil {
			return err
		}

		if err := agentStore.Create(ctx, agent); err != nil {
			return fmt.Errorf("failed to record agent %q: %w", agent.Name, err)
		}

		return nil
	})
}

// tmux silently falls back to its default directory when it cannot enter the requested one.
func requireUsableDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w %q: %w", ErrBadDirectory, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a directory", ErrBadDirectory, dir)
	}

	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("%w %q: %w", ErrBadDirectory, dir, err)
	}
	_ = handle.Close()

	return nil
}

// The store must use the transaction holding the slot lock.
func claimRoomForAgents(ctx context.Context, agents agentClaimer, maxAgents, want int) error {
	if maxAgents <= 0 || want <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForAgents(ctx, agents, maxAgents, want)
}

func requireRoomForAgents(ctx context.Context, agents agentCounter, maxAgents, want int) error {
	if maxAgents <= 0 || want <= 0 {
		return nil
	}

	live, err := agents.CountLive(ctx)
	if err != nil {
		return fmt.Errorf("failed to count the agents already running: %w", err)
	}
	if live+int64(want) > int64(maxAgents) {
		return fmt.Errorf("%w: max number of concurrent agents reached (%d of %d), please archive one to continue", ErrTooManyAgents, live, maxAgents)
	}

	return nil
}

type launchPlan struct {
	name            string
	harness         harness.Harness
	harnessID       string
	dir             string
	flags           string
	workflowContext string
	setupAction     string
	cleanupAction   string
}

func (s *AgentService) planLaunch(ctx context.Context, p NewAgentParams) (*launchPlan, error) {
	wf, err := s.workflows.Get(ctx, p.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", p.WorkflowID, err)
	}
	if wf == nil {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, p.WorkflowID)
	}

	harnessID := strings.TrimSpace(p.Harness)
	if harnessID == "" {
		harnessID = strings.TrimSpace(wf.Harness)
	}
	if harnessID == "" {
		harnessID = s.userDefaultHarness
	}

	hn, err := s.harnesses.GetByID(harnessID)
	if err != nil {
		return nil, err
	}

	dir := config.ExpandHome(strings.TrimSpace(p.Dir))
	if dir == "" {
		dir = config.ExpandHome(strings.TrimSpace(wf.Cwd))
	}
	if dir == "" {
		dir = s.tmux.GetClientPath(p.Client)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	if err := requireUsableDir(dir); err != nil {
		return nil, err
	}

	agentFlags := strings.TrimSpace(p.Flags)
	if agentFlags == "" {
		agentFlags = strings.TrimSpace(wf.Flags)
	}

	setupAction := wf.SetupAction
	if p.SetupAction != nil {
		setupAction = strings.TrimSpace(*p.SetupAction)
	}

	cleanupAction := wf.CleanupAction
	if p.CleanupAction != nil {
		cleanupAction = strings.TrimSpace(*p.CleanupAction)
	}

	return &launchPlan{
		name:            s.uniqueAgentName(ctx, p.WorkflowID, p.Title, p.Prompt),
		harness:         hn,
		harnessID:       harnessID,
		dir:             dir,
		flags:           strings.TrimSpace(s.userDefaultFlags[harnessID] + " " + agentFlags),
		workflowContext: wf.Context,
		setupAction:     setupAction,
		cleanupAction:   cleanupAction,
	}, nil
}

// agent builds the complete row for a launching agent. It is the single place that
// assembles the record; callers pass in whatever they've resolved so far and get back
// the full shape, rather than constructing a partial value and patching fields onto it.
func (plan *launchPlan) agent(params NewAgentParams, id, sessionID, dir, pane string, spawner *agentstore.Agent) agentstore.Agent {
	var spawnedBy *string
	if spawner != nil {
		spawnedBy = &spawner.ID
	}

	return agentstore.Agent{
		ID:               id,
		Name:             plan.name,
		Pane:             pane,
		Cwd:              dir,
		Flags:            plan.flags,
		SetupAction:      params.SetupAction,
		CleanupAction:    params.CleanupAction,
		Prompt:           params.Prompt,
		State:            agentstore.StateIdle,
		Harness:          plan.harnessID,
		SessionID:        sessionID,
		WorkflowID:       params.WorkflowID,
		ScheduleID:       params.ScheduleID,
		IdempotencyKey:   idempotencyKey(params.IdempotencyKey),
		SpawnedByAgentID: spawnedBy,
		Hidden:           params.Hidden,
	}
}

func (s *AgentService) runSetup(ctx context.Context, planned agentstore.Agent, name string) (string, error) {
	if s.lifecycle == nil || name == "" {
		return "", nil
	}

	return s.lifecycle.RunSetupAction(ctx, planned, name)
}

func (s *AgentService) runResumeSetup(ctx context.Context, agent agentstore.Agent) (string, error) {
	name := s.getSetupAction(ctx, agent)
	if s.lifecycle == nil || name == "" {
		return "", nil
	}

	return s.lifecycle.RunResumeAction(ctx, agent, name)
}

func (s *AgentService) setupDir(planned, named string) (string, error) {
	if named == "" {
		return planned, nil
	}

	return namedDir(named)
}

func namedDir(named string) (string, error) {
	if named == "" {
		return "", nil
	}

	dir := config.ExpandHome(named)
	if err := requireUsableDir(dir); err != nil {
		return "", fmt.Errorf("%w: a setup action named %q: %w", ErrSetupFailed, dir, err)
	}

	return dir, nil
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

func (s *AgentService) getWorkflowFor(ctx context.Context, agent agentstore.Agent) *workflow.Workflow {
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

func idempotencyKey(key string) *string {
	if key == "" {
		return nil
	}

	return &key
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

func (s *AgentService) uniqueAgentName(ctx context.Context, workflowID, title, prompt string) string {
	base := agentstore.SanitizeName(strings.TrimSpace(title))
	if base == "" {
		base = xstr.Slug(xstr.FirstLine(prompt), 24)
	}
	if base == "" {
		base = "agent"
	}

	name := base
	for i := 2; s.isNameTaken(ctx, workflowID, name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}

	return name
}

func (s *AgentService) isNameTaken(ctx context.Context, workflowID, name string) bool {
	taken, err := s.agents.ActiveNameExists(ctx, workflowID, name)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to check whether an agent name is taken",
			append([]slog.Attr{slog.String(logging.WorkflowID, workflowID)}, logging.Err(err)...)...)
	}

	return taken
}
