package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"weave/config"
	"weave/internal/logging"
	"weave/internal/store"
	"weave/lib/xslice"
	"weave/lib/xstr"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	"weave/internal/store/transcript"
	"weave/internal/store/workflow"
)

var ErrAgentNotFound = errors.New("agent not found")
var ErrWorkflowNotFound = errors.New("workflow not found")
var ErrBadDirectory = errors.New("unusable working directory")
var ErrTooManyAgents = errors.New("too many agents")
var ErrAgentArchived = errors.New("agent is archived")

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

	launch := newAgentLaunch(s, p, plan, id, plan.harness.MintSessionID())

	if err := launch.runSetup(ctx); err != nil {
		return nil, err
	}

	// Setup may create resources before any agent row exists.
	launched := false
	defer func() {
		if !launched {
			launch.unwind(ctx)
		}
	}()

	if err := launch.openPane(); err != nil {
		return nil, err
	}

	// Claim the slot with the insert because setup cannot run inside the transaction.
	created, err := launch.createRow(ctx)
	if err != nil {
		return nil, err
	}

	if err := launch.start(); err != nil {
		if deleteErr := launch.removeRow(ctx); deleteErr != nil {
			return nil, fmt.Errorf("failed to launch the agent for %q (%w), and failed to clean up: %w", plan.name, err, deleteErr)
		}

		return nil, fmt.Errorf("failed to launch the agent for %q: %w", plan.name, err)
	}

	launched = true

	return created, nil
}

type agentLaunch struct {
	svc       *AgentService
	params    NewAgentParams
	plan      *launchPlan
	id        string
	sessionID string
	agent     agentstore.Agent
	setupName string
	setupRan  bool
	pane      string
}

func newAgentLaunch(svc *AgentService, params NewAgentParams, plan *launchPlan, id, sessionID string) *agentLaunch {
	return &agentLaunch{
		svc:       svc,
		params:    params,
		plan:      plan,
		id:        id,
		sessionID: sessionID,
		agent:     plan.agent(params, id, sessionID),
	}
}

// runSetup runs the workflow's setup action, if any, against the planned agent.
func (l *agentLaunch) runSetup(ctx context.Context) error {
	named, err := l.svc.runSetup(ctx, l.agent, l.plan.setupAction)
	if err != nil {
		return err
	}

	l.setupName = named
	l.setupRan = true

	return nil
}

// openPane resolves the working directory, opens the tmux window, and stamps
// it with the agent's ID.
func (l *agentLaunch) openPane() error {
	dir, err := l.svc.setupDir(l.plan.dir, l.setupName)
	if err != nil {
		return err
	}
	l.agent.Cwd = dir

	pane, err := l.svc.tmux.NewWindow(ManagerSession, l.plan.name, dir)
	if err != nil {
		return fmt.Errorf("failed to open a window for %q: %w", l.plan.name, err)
	}
	l.pane = pane
	l.agent.Pane = pane

	if err := l.svc.tmux.SetPaneOwner(pane, l.id); err != nil {
		return fmt.Errorf("failed to stamp the pane for %q: %w", l.plan.name, err)
	}

	return nil
}

// createRow claims a capacity slot and inserts the agent record in one
// transaction, then reads the row back.
func (l *agentLaunch) createRow(ctx context.Context) (*agentstore.Agent, error) {
	if err := l.svc.createUnderCeiling(ctx, l.agent); err != nil {
		return nil, err
	}

	created, err := l.svc.agents.GetInWorkflow(ctx, l.params.WorkflowID, l.plan.name)
	if err != nil {
		return nil, fmt.Errorf("failed to read back agent %q: %w", l.plan.name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("agent %q vanished after being recorded", l.plan.name)
	}

	return created, nil
}

// start hands the pane to the harness launcher.
func (l *agentLaunch) start() error {
	return l.svc.launcher.launch(l.plan.harness, launchParams{
		Pane:            l.pane,
		Dir:             l.agent.Cwd,
		Flags:           l.plan.flags,
		Prompt:          l.params.Prompt,
		WorkflowContext: l.plan.workflowContext,
		SessionID:       l.sessionID,
		AgentID:         l.id,
	})
}

// unwind reverses whatever of the pane and setup action actually ran. The
// agent row is removed separately, by removeRow, only once launching has
// actually failed.
func (l *agentLaunch) unwind(ctx context.Context) {
	if l.pane != "" {
		l.svc.tmux.KillPane(l.pane)
	}
	if l.setupRan {
		l.svc.runCleanup(ctx, l.agent, l.plan.cleanupAction)
	}
}

func (l *agentLaunch) removeRow(ctx context.Context) error {
	return l.svc.removeAgents(ctx, []string{l.id})
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

	return s.removeAgents(ctx, ids)
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

	return nil
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

	return &launchPlan{
		name:            s.uniqueAgentName(ctx, p.WorkflowID, p.Title, p.Prompt),
		harness:         hn,
		harnessID:       harnessID,
		dir:             dir,
		flags:           strings.TrimSpace(s.userDefaultFlags[harnessID] + " " + agentFlags),
		workflowContext: wf.Context,
		setupAction:     wf.SetupAction,
		cleanupAction:   wf.CleanupAction,
	}, nil
}

func (plan *launchPlan) agent(params NewAgentParams, id, sessionID string) agentstore.Agent {
	return agentstore.Agent{
		ID:             id,
		Name:           plan.name,
		Cwd:            plan.dir,
		Flags:          plan.flags,
		Prompt:         params.Prompt,
		State:          agentstore.StateIdle,
		Harness:        plan.harnessID,
		SessionID:      sessionID,
		WorkflowID:     params.WorkflowID,
		ScheduleID:     params.ScheduleID,
		IdempotencyKey: idempotencyKey(params.IdempotencyKey),
		Hidden:         params.Hidden,
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
	wf := s.getWorkflowFor(ctx, agent)
	if wf == nil {
		return ""
	}

	return wf.CleanupAction
}

func (s *AgentService) getSetupAction(ctx context.Context, agent agentstore.Agent) string {
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
