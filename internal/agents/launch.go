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
	"weave/internal/harness"
	"weave/internal/logging"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"
	"weave/lib/xstr"
)

// launchAgentStore is the slice of the agent store a launch writes to, from the key it spends
// through to the row it reads back.
type launchAgentStore interface {
	agentGetter
	agentCounter
	childCounter
	nameChecker
	agentFailWriter
	GetByIdempotencyKey(ctx context.Context, key string) (*agentstore.Agent, error)
	ReleaseIdempotencyKey(ctx context.Context, key string) (bool, error)
	GetInWorkflow(ctx context.Context, workflowID, name string) (*agentstore.Agent, error)
}

type LaunchService struct {
	tmux               paneController
	agents             launchAgentStore
	workflows          workflowReader
	tx                 txRunner
	launcher           *Launcher
	harnesses          harnessGetter
	actions            agentActions
	userDefaultHarness string
	userDefaultFlags   map[string]string
	maxAgents          int
	maxChildren        int
}

type LaunchServiceParams struct {
	Tmux               paneController
	Agents             launchAgentStore
	Workflows          workflowReader
	Tx                 txRunner
	Launcher           *Launcher
	Harnesses          harnessGetter
	Actions            agentActions
	UserDefaultHarness string
	UserDefaultFlags   map[string]string
	MaxAgents          int
	MaxChildren        int
}

// NewLaunchService builds the service from its dependencies.
func NewLaunchService(p LaunchServiceParams) *LaunchService {
	return &LaunchService{
		tmux:               p.Tmux,
		agents:             p.Agents,
		workflows:          p.Workflows,
		tx:                 p.Tx,
		launcher:           p.Launcher,
		harnesses:          p.Harnesses,
		actions:            p.Actions,
		userDefaultHarness: p.UserDefaultHarness,
		userDefaultFlags:   p.UserDefaultFlags,
		maxAgents:          p.MaxAgents,
		maxChildren:        p.MaxChildren,
	}
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
	ParentAgentID  string
}

// NewAgent creates and launches a background agent, answering a key already spent on a launch
// with that agent and ErrAlreadyLaunched rather than a bare nil.
func (s *LaunchService) NewAgent(ctx context.Context, p NewAgentParams) (*agentstore.Agent, error) {
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

	var parent *agentstore.Agent
	if p.ParentAgentID != "" {
		found, err := s.findParent(ctx, p)
		if err != nil {
			return nil, err
		}
		parent = found
		p = childParams(p, *found)
	}

	if err := requireRoomForAgents(ctx, s.agents, s.maxAgents, 1); err != nil {
		return nil, err
	}

	return s.launchAgent(ctx, p, spawner, parent)
}

// releaseFailedLaunch frees an idempotency key held by a create that failed, so the next
// attempt launches instead of being answered with the corpse.
func (s *LaunchService) releaseFailedLaunch(ctx context.Context, existing agentstore.Agent, key string) (bool, error) {
	// A failed cleanup belongs to an agent that ran, so only a failed create launched nothing.
	if existing.State != agentstore.StateFailedToCreate {
		return false, nil
	}

	// The row is left where it stands rather than relaunched into, since its id names the
	// worktree and branch the cleanup action is still tearing down in the background.
	return s.agents.ReleaseIdempotencyKey(ctx, key)
}

func (s *LaunchService) findSpawner(ctx context.Context, id string) (*agentstore.Agent, error) {
	spawner, err := s.agents.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrSpawnerUnknown, id)
	}
	if err != nil {
		return nil, err
	}

	return spawner, nil
}

// findParent resolves the agent a child is being attached to, and refuses a request that asks
// for anything the parent decides instead: the checkout, the two actions, and the workflow are
// its parent's rather than the caller's to choose, so a request naming one of them is answered
// rather than quietly overridden.
func (s *LaunchService) findParent(ctx context.Context, p NewAgentParams) (*agentstore.Agent, error) {
	parent, err := s.agents.GetByID(ctx, p.ParentAgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrParentNotFound, p.ParentAgentID)
	}
	if err != nil {
		return nil, err
	}
	if parent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q", ErrAgentArchived, parent.Name)
	}
	if parent.ParentAgentID != nil {
		return nil, fmt.Errorf("%w: %q is itself attached to another agent", ErrParentIsChild, parent.Name)
	}

	// This is ahead of what the request asked for so that the refusal names the parent's missing
	// directory rather than reporting a conflict with a directory that is the empty string.
	if parent.Cwd == "" {
		return nil, fmt.Errorf("%w: %q has none recorded", ErrChildNeedsParentDirectory, parent.Name)
	}

	switch {
	case strings.TrimSpace(p.Dir) != "":
		return nil, fmt.Errorf("%w: it runs in %q, its parent's own checkout", ErrChildOwnsLaunch, parent.Cwd)
	case p.SetupAction != nil && strings.TrimSpace(*p.SetupAction) != "":
		return nil, fmt.Errorf("%w: it runs no setup action, since the checkout is already there", ErrChildOwnsLaunch)
	case p.CleanupAction != nil && strings.TrimSpace(*p.CleanupAction) != "":
		return nil, fmt.Errorf("%w: it runs no cleanup action, since the checkout is not its to tear down", ErrChildOwnsLaunch)
	case p.WorkflowID != parent.WorkflowID:
		return nil, fmt.Errorf("%w: it is filed in its parent's workflow %q", ErrChildOwnsLaunch, parent.WorkflowID)
	case p.ScheduleID != nil:
		return nil, fmt.Errorf("%w: a schedule launches into a workflow rather than onto an agent", ErrChildOwnsLaunch)
	}

	return parent, nil
}

// childParams holds a child to what its parent gives it, writing the blank actions onto the row
// rather than leaving them unset, since an unset action falls back to the workflow's and for a
// worktree-enabled workflow that one would tear the parent's checkout down from under it.
func childParams(p NewAgentParams, parent agentstore.Agent) NewAgentParams {
	p.Dir = parent.Cwd
	p.SetupAction = xptr.Ptr("")
	p.CleanupAction = xptr.Ptr("")

	return p
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

func (s *LaunchService) planLaunch(ctx context.Context, p NewAgentParams) (*launchPlan, error) {
	wf, err := s.workflows.Get(ctx, p.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, p.WorkflowID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", p.WorkflowID, err)
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
		IdempotencyKey:   nullable(params.IdempotencyKey),
		SpawnedByAgentID: spawnedBy,
		ParentAgentID:    nullable(params.ParentAgentID),
		Hidden:           params.Hidden,
		AdoptedBranch:    params.AdoptedBranch,
	}
}

func (s *LaunchService) launchAgent(ctx context.Context, p NewAgentParams, spawner, parent *agentstore.Agent) (result *agentstore.Agent, launchErr error) {
	plan, err := s.planLaunch(ctx, p)
	if err != nil {
		return nil, err
	}

	id, err := store.NewID()
	if err != nil {
		return nil, err
	}
	sessionID := plan.harness.MintSessionID()
	agent := plan.agent(p, id, sessionID, plan.dir, "", spawner)
	agent.State = agentstore.StateSettingUp

	if err := s.createUnderCeiling(ctx, agent, parent); err != nil {
		return nil, err
	}

	var pane string
	pastSetup := false
	launched := false
	defer func() {
		if launched {
			return
		}

		// Nothing else in weave moves a row out of setting_up, so closing out a failed
		// create cannot depend on the caller still being around to see it happen.
		teardown, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
		defer cancel()

		if pane != "" {
			s.tmux.KillPane(pane)
		}
		if pastSetup {
			runCleanup(teardown, s.actions, agent, plan.cleanupAction, nil)
		}
		s.failCreate(teardown, agent, launchErr)
	}()

	setupName, err := s.runSetup(ctx, agent, plan.setupAction, p.SetupPane)
	if err != nil {
		return nil, err
	}
	pastSetup = true

	dir, err := s.setupDir(plan.dir, setupName)
	if err != nil {
		return nil, err
	}
	agent = plan.agent(p, id, sessionID, dir, "", spawner)

	pane, err = s.tmux.NewWindow(ManagerSession, plan.name, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open a window for %q: %w", plan.name, err)
	}
	agent = plan.agent(p, id, sessionID, dir, pane, spawner)

	if err := s.tmux.SetPaneOwner(pane, id); err != nil {
		return nil, fmt.Errorf("failed to stamp the pane for %q: %w", plan.name, err)
	}

	if err := s.agents.Update(ctx, agent); err != nil {
		return nil, fmt.Errorf("failed to record agent %q: %w", plan.name, err)
	}

	created, err := s.agents.GetInWorkflow(ctx, p.WorkflowID, plan.name)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("agent %q vanished after being recorded", plan.name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read back agent %q: %w", plan.name, err)
	}

	params := launchParams{
		Pane:            pane,
		Dir:             agent.Cwd,
		Flags:           plan.flags,
		Prompt:          p.Prompt,
		WorkflowContext: plan.workflowContext,
		SessionID:       sessionID,
		AgentID:         id,
	}
	if spawner != nil {
		params.SpawnedByAgentID = spawner.ID
		params.SpawnedByName = spawner.Name
	}
	if parent != nil {
		params.ParentAgentID = parent.ID
		params.ParentName = parent.Name
	}

	if err := s.launcher.launch(plan.harness, params); err != nil {
		return nil, fmt.Errorf("failed to launch the agent for %q: %w", plan.name, err)
	}

	launched = true

	return created, nil
}

// teardownTimeout stands in for the caller's own deadline once that no longer applies, so a
// wedged database cannot hold a failed create open indefinitely.
const teardownTimeout = 30 * time.Second

func (s *LaunchService) failCreate(ctx context.Context, agent agentstore.Agent, cause error) {
	failAs(ctx, s.agents, agent, agentstore.StateFailedToCreate, "create", cause)
}

func (s *LaunchService) runSetup(ctx context.Context, planned agentstore.Agent, name, pane string) (string, error) {
	if s.actions == nil || name == "" {
		return "", nil
	}

	return s.actions.RunSetupAction(ctx, planned, name, pane)
}

func (s *LaunchService) setupDir(planned, named string) (string, error) {
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

func (s *LaunchService) createUnderCeiling(ctx context.Context, agent agentstore.Agent, parent *agentstore.Agent) error {
	return s.tx.InTx(ctx, func(db store.DBTX) error {
		agentStore := agentstore.New(db)

		if err := claimRoomForAgents(ctx, agentStore, s.maxAgents, 1); err != nil {
			return err
		}
		if err := claimRoomForChildren(ctx, agentStore, s.maxChildren, parent); err != nil {
			return err
		}

		if err := agentStore.Create(ctx, agent); err != nil {
			return fmt.Errorf("failed to record agent %q: %w", agent.Name, err)
		}

		return nil
	})
}

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

func claimRoomForAgents(ctx context.Context, agents agentSlots, maxAgents, want int) error {
	if maxAgents <= 0 || want <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForAgents(ctx, agents, maxAgents, want)
}

// claimRoomForChildren holds one parent to the children it may run at once, which is a lower
// ceiling than the panel's because they all edit the one checkout.
func claimRoomForChildren(ctx context.Context, agents childSlots, maxChildren int, parent *agentstore.Agent) error {
	if parent == nil || maxChildren <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForChildren(ctx, agents, maxChildren, []parentRoom{{
		id:   parent.ID,
		name: parent.Name,
		want: 1,
	}})
}

// claimRoomForRestoredChildren holds every parent a restoring batch hangs a child under to the
// same ceiling a create is held to, since a child archived on its own may have been replaced
// while it was gone.
func claimRoomForRestoredChildren(ctx context.Context, agents restoredChildSlots, maxChildren int, restoring []agentstore.Agent) error {
	if maxChildren <= 0 {
		return nil
	}

	// The whole batch is counted against each parent at once, since children checked one at a
	// time would each find room and walk past the ceiling together.
	want := make(map[string]int, len(restoring))
	var parentIDs []string
	for _, agent := range restoring {
		if agent.ParentAgentID == nil {
			continue
		}
		if want[*agent.ParentAgentID] == 0 {
			parentIDs = append(parentIDs, *agent.ParentAgentID)
		}
		want[*agent.ParentAgentID]++
	}
	if len(parentIDs) == 0 {
		return nil
	}

	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	parents, err := agents.ListByID(ctx, parentIDs)
	if err != nil {
		return fmt.Errorf("failed to look up %d parents of the restoring children: %w", len(parentIDs), err)
	}

	wanted := make([]parentRoom, 0, len(parents))
	for _, parent := range parents {
		wanted = append(wanted, parentRoom{
			id:   parent.ID,
			name: parent.Name,
			want: want[parent.ID],
		})
	}

	return requireRoomForChildren(ctx, agents, maxChildren, wanted)
}

// parentRoom is one parent and how many children a call wants to put under it.
type parentRoom struct {
	id   string
	name string
	want int
}

func requireRoomForChildren(ctx context.Context, agents childCounter, maxChildren int, wanted []parentRoom) error {
	for _, parent := range wanted {
		held, err := agents.CountLiveChildren(ctx, parent.id)
		if err != nil {
			return fmt.Errorf("failed to count the children of agent %q: %w", parent.name, err)
		}
		if held+parent.want > maxChildren {
			return fmt.Errorf("%w: %q may run %d at once and this would make %d, and they share one checkout",
				ErrTooManyChildren, parent.name, maxChildren, held+parent.want)
		}
	}

	return nil
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

func (s *LaunchService) uniqueAgentName(ctx context.Context, workflowID, title, prompt string) string {
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

func (s *LaunchService) isNameTaken(ctx context.Context, workflowID, name string) bool {
	taken, err := s.agents.ActiveNameExists(ctx, workflowID, name)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to check whether an agent name is taken",
			append([]slog.Attr{slog.String(logging.WorkflowID, workflowID)}, logging.Err(err)...)...)
	}

	return taken
}

// nullable is a column the row leaves null when the launch names nothing for it.
func nullable(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}
