package agents

import (
	"cmp"
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
	"weave/internal/scripts"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/watch"
	"weave/lib/xptr"
	"weave/lib/xslice"
	"weave/lib/xstr"
)

type agentCounter interface {
	CountLive(ctx context.Context) (int64, error)
}

type childCounter interface {
	CountLiveChildren(ctx context.Context, parentID string) (int, error)
}

type childSlotLocker interface {
	childCounter
	LockSlots(ctx context.Context) error
}

// restoredChildSlotLocker is what a restore holds a parent to its ceiling with: the children it
// already runs, plus the rows a restoring batch would hang under it.
type restoredChildSlotLocker interface {
	childSlotLocker
	ListByID(ctx context.Context, ids []string) ([]agentstore.Agent, error)
}

type agentSlotLocker interface {
	agentCounter
	LockSlots(ctx context.Context) error
}

// launchAgentReadWriter is the slice of the agent store a launch writes to.
type launchAgentReadWriter interface {
	agentGetter
	agentCounter
	childCounter
	childLister
	nameChecker
	agentFailWriter
	GetByIdempotencyKey(ctx context.Context, key string) (*agentstore.Agent, error)
	ReleaseIdempotencyKey(ctx context.Context, key string) (bool, error)
	GetInWorkflow(ctx context.Context, workflowID, name string) (*agentstore.Agent, error)
	MarkSettingUpIfFailedToCreate(ctx context.Context, id string) (bool, error)
	SetHarness(ctx context.Context, params agentstore.SetHarnessParams) error
}

type LaunchService struct {
	tmux               paneController
	agents             launchAgentReadWriter
	reviews            liveReviewerFinder
	workflows          workflowReader
	tx                 txRunner
	launcher           *Launcher
	harnesses          harnessGetter
	scripts            agentScriptRunner
	watch              watch.Publisher
	userDefaultHarness string
	userDefaultFlags   map[string]string
	maxAgents          int
	maxChildren        int
}

type NewLaunchServiceParams struct {
	Tmux      paneController
	Agents    launchAgentReadWriter
	Reviews   liveReviewerFinder
	Workflows workflowReader
	Tx        txRunner
	Launcher  *Launcher
	Harnesses harnessGetter
	Scripts   agentScriptRunner
	// Watch is told which rows moved, watch.Discard when unset.
	Watch              watch.Publisher
	UserDefaultHarness string
	UserDefaultFlags   map[string]string
	MaxAgents          int
	MaxChildren        int
}

// NewLaunchService builds the service from its dependencies.
func NewLaunchService(params NewLaunchServiceParams) *LaunchService {
	return &LaunchService{
		tmux:               params.Tmux,
		agents:             params.Agents,
		reviews:            params.Reviews,
		workflows:          params.Workflows,
		tx:                 params.Tx,
		launcher:           params.Launcher,
		harnesses:          params.Harnesses,
		scripts:            params.Scripts,
		watch:              cmp.Or(params.Watch, watch.Discard),
		userDefaultHarness: params.UserDefaultHarness,
		userDefaultFlags:   params.UserDefaultFlags,
		maxAgents:          params.MaxAgents,
		maxChildren:        params.MaxChildren,
	}
}

type NewAgentParams struct {
	Title            string
	Dir              string
	Flags            string
	ProvisionEnabled *bool
	Prompt           string
	Harness          string
	WorkflowID       string
	ScheduleID       *string
	Hidden           bool
	IdempotencyKey   string
	AdoptedBranch    string
	ParentAgentID    string
	Role             agentstore.Role
}

// NewAgent creates and launches a background agent, answering a key already spent on a launch
// with that agent and ErrAlreadyLaunched.
func (s *LaunchService) NewAgent(ctx context.Context, params NewAgentParams) (*agentstore.Agent, error) {
	if params.IdempotencyKey != "" {
		existing, err := s.agents.GetByIdempotencyKey(ctx, params.IdempotencyKey)
		switch {
		case err == nil:
			// The row is written before the window is opened, so a create that failed leaves the
			// key spent on an agent that never ran and a retry answered with a corpse.
			released, err := s.releaseFailedLaunch(ctx, *existing, params.IdempotencyKey)
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

		if err := requireOwnWorkflow(*found, params); err != nil {
			return nil, err
		}
	}

	var parent *agentstore.Agent
	if params.ParentAgentID != "" {
		found, err := s.findParent(ctx, params)
		if err != nil {
			return nil, err
		}
		parent = found
		params = childParams(params, *found)
	}

	if err := requireRoomForAgents(ctx, s.agents, s.maxAgents, 1); err != nil {
		return nil, err
	}

	return s.launchAgent(ctx, params, spawner, parent)
}

// releaseFailedLaunch frees an idempotency key held by a create that failed, so the next
// attempt launches.
func (s *LaunchService) releaseFailedLaunch(ctx context.Context, existing agentstore.Agent, key string) (bool, error) {
	// A failed cleanup belongs to an agent that ran, so only a failed create launched nothing.
	if existing.State != agentstore.StateFailedToCreate {
		return false, nil
	}

	// The row is left where it stands rather than relaunched into, its id naming the worktree and
	// branch the deprovisioner is still tearing down.
	released, err := s.agents.ReleaseIdempotencyKey(ctx, key)
	if released {
		s.watch.TouchAgents(existing.ID)
	}

	return released, err
}

// requireOwnWorkflow holds an orchestrator to the workflow it stands behind.
func requireOwnWorkflow(spawner agentstore.Agent, params NewAgentParams) error {
	if spawner.Role != agentstore.RoleOrchestrator || params.WorkflowID == spawner.WorkflowID {
		return nil
	}

	return fmt.Errorf("%w: %q stands behind workflow %q", ErrOutsideWorkflow, spawner.Name, spawner.WorkflowID)
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

// findParent resolves the agent a child is being attached to, and refuses a request naming the
// checkout, the provisioner or the workflow, all of which the parent decides.
func (s *LaunchService) findParent(ctx context.Context, params NewAgentParams) (*agentstore.Agent, error) {
	parent, err := s.agents.GetByID(ctx, params.ParentAgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrParentNotFound, params.ParentAgentID)
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
	if parent.Role == agentstore.RoleOrchestrator {
		return nil, fmt.Errorf(
			"%w: %q is drawn on its workflow's own row, so a child of it would have no row anywhere; spawn an ordinary agent into the workflow with provisionEnabled false instead",
			ErrParentIsOrchestrator, parent.Name)
	}

	// Ahead of what the request asked for, which puts the parent's missing directory in the
	// refusal rather than a conflict with an empty one.
	if parent.Cwd == "" {
		return nil, fmt.Errorf("%w: %q has none recorded", ErrChildNeedsParentDirectory, parent.Name)
	}

	switch {
	case strings.TrimSpace(params.Dir) != "":
		return nil, fmt.Errorf("%w: it runs in %q, its parent's own checkout", ErrChildOwnsLaunch, parent.Cwd)
	case params.ProvisionEnabled != nil && *params.ProvisionEnabled:
		return nil, fmt.Errorf("%w: it provisions nothing, since the checkout is already there", ErrChildOwnsLaunch)
	case params.WorkflowID != parent.WorkflowID:
		return nil, fmt.Errorf("%w: it is filed in its parent's workflow %q", ErrChildOwnsLaunch, parent.WorkflowID)
	case params.ScheduleID != nil:
		return nil, fmt.Errorf("%w: a schedule launches into a workflow rather than onto an agent", ErrChildOwnsLaunch)
	}

	return parent, nil
}

// childParams holds a child to what its parent gives it, writing the disabled provisioner onto
// the row.
func childParams(params NewAgentParams, parent agentstore.Agent) NewAgentParams {
	params.Dir = parent.Cwd
	params.ProvisionEnabled = xptr.Ptr(false)

	return params
}

type launchPlan struct {
	name            string
	harness         harness.Harness
	harnessID       string
	dir             string
	flags           string
	workflowContext string
	provisioner     string
	deprovisioner   string
	hooks           []workflowstore.Hook
}

func (s *LaunchService) planLaunch(ctx context.Context, params NewAgentParams) (*launchPlan, error) {
	wf, err := s.workflows.Get(ctx, params.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrWorkflowNotFound.Saying("no workflow with id %q", params.WorkflowID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", params.WorkflowID, err)
	}

	workflowHarness := s.workflowHarness(wf)

	harnessID := strings.TrimSpace(params.Harness)
	if harnessID == "" {
		harnessID = workflowHarness
	}

	hn, err := s.harnesses.GetByID(harnessID)
	if err != nil {
		return nil, err
	}

	dir := config.ExpandHome(strings.TrimSpace(params.Dir))
	if dir == "" {
		dir = config.ExpandHome(strings.TrimSpace(wf.Cwd))
	}
	if dir == "" {
		dir = s.tmux.GetHostPath()
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	if err := scripts.RequireUsableDir(dir); err != nil {
		return nil, err
	}

	// The worktree is weave's own, so an agent gets the built-in pair or nothing at all.
	provisioner, deprovisioner := "", ""
	provisions := wf.Worktree.Enabled && !IsProvisionerDisabled(params.ProvisionEnabled)
	if provisions {
		provisioner, deprovisioner = worktreeProvisioner, worktreeDeprovisioner
	}

	// The hooks a workflow lists are read only for the agents that own their own directory, and a
	// child or an agent told to keep the directory it was given was handed somebody else's.
	hooks := wf.Hooks
	if params.ParentAgentID != "" || IsProvisionerDisabled(params.ProvisionEnabled) {
		hooks = nil
	}

	return &launchPlan{
		name:      s.uniqueAgentName(ctx, params.WorkflowID, params.Title, params.Prompt),
		harness:   hn,
		harnessID: harnessID,
		dir:       dir,
		flags: s.launchFlags(launchFlagsParams{
			harnessID:       harnessID,
			workflowHarness: workflowHarness,
			agentFlags:      params.Flags,
			workflowFlags:   wf.Flags,
		}),
		workflowContext: wf.Context,
		provisioner:     provisioner,
		deprovisioner:   deprovisioner,
		hooks:           hooks,
	}, nil
}

type launchFlagsParams struct {
	harnessID       string
	workflowHarness string
	agentFlags      string
	workflowFlags   string
}

// launchFlags is what an agent is launched with, the user's defaults for the CLI it runs ahead
// of the flags it was given.
func (s *LaunchService) launchFlags(params launchFlagsParams) string {
	// A workflow's flags are written for the CLI it names, so an agent running a different one
	// is launched without them.
	flags := strings.TrimSpace(params.agentFlags)
	if flags == "" && params.harnessID == params.workflowHarness {
		flags = strings.TrimSpace(params.workflowFlags)
	}

	return strings.TrimSpace(s.userDefaultFlags[params.harnessID] + " " + flags)
}

// workflowHarness is the CLI a workflow's agents take when they name none of their own.
func (s *LaunchService) workflowHarness(wf *workflowstore.Workflow) string {
	if wf == nil {
		return s.userDefaultHarness
	}
	if named := strings.TrimSpace(wf.Harness); named != "" {
		return named
	}

	return s.userDefaultHarness
}

func (p *launchPlan) agent(params NewAgentParams, id, sessionID, dir, pane string, spawner *agentstore.Agent) agentstore.Agent { //nolint:revive // a params struct is the follow-up
	var spawnedBy *string
	if spawner != nil {
		spawnedBy = &spawner.ID
	}

	return agentstore.Agent{
		ID:               id,
		Name:             p.name,
		Pane:             pane,
		Cwd:              dir,
		Flags:            p.flags,
		ProvisionEnabled: params.ProvisionEnabled,
		Prompt:           params.Prompt,
		State:            agentstore.StateIdle,
		Harness:          p.harnessID,
		SessionID:        sessionID,
		WorkflowID:       params.WorkflowID,
		ScheduleID:       params.ScheduleID,
		IdempotencyKey:   nullable(params.IdempotencyKey),
		SpawnedByAgentID: spawnedBy,
		ParentAgentID:    nullable(params.ParentAgentID),
		Hidden:           params.Hidden,
		AdoptedBranch:    params.AdoptedBranch,
		Role:             params.Role,
	}
}

func (s *LaunchService) launchAgent(ctx context.Context, params NewAgentParams, spawner, parent *agentstore.Agent) (result *agentstore.Agent, launchErr error) {
	plan, err := s.planLaunch(ctx, params)
	if err != nil {
		return nil, err
	}

	id, err := store.NewID()
	if err != nil {
		return nil, err
	}
	sessionID := plan.harness.MintSessionID()
	agent := plan.agent(params, id, sessionID, plan.dir, "", spawner)
	agent.State = agentstore.StateSettingUp

	if err := s.createUnderCeiling(ctx, agent, parent); err != nil {
		return nil, err
	}
	s.watch.TouchAgents(agent.ID)

	return s.startInPane(ctx, agent, resolvedLaunch{
		harness:         plan.harness,
		provisioner:     plan.provisioner,
		deprovisioner:   plan.deprovisioner,
		hooks:           plan.hooks,
		defaultDir:      plan.dir,
		workflowContext: plan.workflowContext,
		spawner:         spawner,
		parent:          parent,
	})
}

type resolvedLaunch struct {
	harness         harness.Harness
	provisioner     string
	deprovisioner   string
	hooks           []workflowstore.Hook
	defaultDir      string
	workflowContext string
	spawner         *agentstore.Agent
	parent          *agentstore.Agent
}

func (s *LaunchService) startInPane(ctx context.Context, agent agentstore.Agent, resolved resolvedLaunch) (result *agentstore.Agent, launchErr error) {
	var pane string
	provisioned := false
	launched := false
	defer func() {
		if launched {
			return
		}

		teardown, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
		defer cancel()

		if pane != "" {
			s.tmux.KillPane(pane)
		}
		if provisioned {
			awaitDeprovision(teardown, s.scripts, agent, resolved.deprovisioner)
		}
		s.failCreate(teardown, agent, launchErr)
	}()

	named, err := s.runProvision(ctx, agent, resolved.provisioner)
	if err != nil {
		return nil, err
	}
	provisioned = resolved.provisioner != ""

	dir, err := namedDir(named)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		dir = resolved.defaultDir
	}
	agent.Cwd = dir

	if err := runHooks(ctx, hookRunParams{
		scripts: s.scripts,
		agent:   agent,
		hooks:   resolved.hooks,
		event:   workflowstore.HookPreLaunch,
	}); err != nil {
		return nil, err
	}

	pane, err = s.tmux.NewWindow(ManagerSession, agent.Name, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open a window for %q: %w", agent.Name, err)
	}

	if err := s.tmux.SetPaneOwner(pane, agent.ID); err != nil {
		return nil, fmt.Errorf("failed to stamp the pane for %q: %w", agent.Name, err)
	}

	agent.Pane = pane
	agent.State = agentstore.StateIdle
	if err := s.agents.Update(ctx, agent); err != nil {
		return nil, fmt.Errorf("failed to record agent %q: %w", agent.Name, err)
	}
	s.watch.TouchAgents(agent.ID)

	launch := launchParams{
		Pane:            pane,
		Dir:             dir,
		Flags:           agent.Flags,
		Prompt:          agent.Prompt,
		WorkflowContext: resolved.workflowContext,
		SessionID:       agent.SessionID,
		AgentID:         agent.ID,
		Role:            agent.Role,
		OwnsCheckout:    resolved.provisioner != "",
	}
	if resolved.spawner != nil {
		launch.SpawnedByAgentID = resolved.spawner.ID
		launch.SpawnedByName = resolved.spawner.Name
	}
	if resolved.parent != nil {
		launch.ParentAgentID = resolved.parent.ID
		launch.ParentName = resolved.parent.Name
	}

	if err := s.launcher.launch(resolved.harness, launch); err != nil {
		return nil, fmt.Errorf("failed to launch the agent for %q: %w", agent.Name, err)
	}

	launched = true

	startHooks(ctx, hookRunParams{
		scripts: s.scripts,
		agent:   agent,
		hooks:   resolved.hooks,
		event:   workflowstore.HookPostLaunch,
	})

	created, err := s.agents.GetByID(ctx, agent.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to read back agent %q: %w", agent.Name, err)
	}

	return created, nil
}

// teardownTimeout stands in for the caller's own deadline once that no longer applies.
const teardownTimeout = 30 * time.Second

func (s *LaunchService) failCreate(ctx context.Context, agent agentstore.Agent, cause error) {
	failAs(ctx, s.agents, agent, agentstore.StateFailedToCreate, "create", cause)
	s.watch.TouchAgents(agent.ID)
}

// awaitDeprovision runs a deprovisioner and waits for it, so a create that failed gives back
// the directory it provisioned before the row is closed out.
func awaitDeprovision(ctx context.Context, deprovision agentScriptRunner, agent agentstore.Agent, name string) {
	finished := make(chan error, 1)
	runDeprovision(ctx, deprovision, agent, name, func(deprovisionErr error) {
		finished <- deprovisionErr
	})

	select {
	case deprovisionErr := <-finished:
		if deprovisionErr != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "the deprovisioner of a failed create did not finish",
				append([]slog.Attr{
					slog.String(logging.ScriptName, name),
					slog.String(logging.ScriptAgentID, agent.ID),
					slog.String(logging.ScriptAgentCwd, agent.Cwd),
				}, logging.Err(deprovisionErr)...)...)
		}
	case <-ctx.Done():
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "gave up waiting for the deprovisioner of a failed create",
			slog.String(logging.ScriptName, name),
			slog.String(logging.ScriptAgentID, agent.ID),
			slog.String(logging.ScriptAgentCwd, agent.Cwd))
	}
}

func (s *LaunchService) runProvision(ctx context.Context, planned agentstore.Agent, name string) (string, error) {
	if s.scripts == nil || name == "" {
		return "", nil
	}

	return s.scripts.RunProvision(ctx, planned, name)
}

func namedDir(named string) (string, error) {
	if named == "" {
		return "", nil
	}

	dir := config.ExpandHome(named)
	if err := scripts.RequireUsableDir(dir); err != nil {
		return "", fmt.Errorf("%w: a provisioner named %q: %w", ErrProvisionFailed, dir, err)
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

func claimRoomForAgents(ctx context.Context, agents agentSlotLocker, maxAgents, want int) error {
	if maxAgents <= 0 || want <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForAgents(ctx, agents, maxAgents, want)
}

// claimRoomForChildren holds one parent to the children it may run at once, a lower ceiling
// than the panel's own.
func claimRoomForChildren(ctx context.Context, agents childSlotLocker, maxChildren int, parent *agentstore.Agent) error {
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
// ceiling a create is held to.
func claimRoomForRestoredChildren(ctx context.Context, agents restoredChildSlotLocker, maxChildren int, restoring []agentstore.Agent) error {
	if maxChildren <= 0 {
		return nil
	}

	// The whole batch is counted against each parent at once, which children checked one at a time
	// would walk past together.
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

	wanted := xslice.Map(parents, func(parent agentstore.Agent) parentRoom {
		return parentRoom{
			id:   parent.ID,
			name: parent.Name,
			want: want[parent.ID],
		}
	})

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
	taken, err := s.agents.ExistsActiveName(ctx, workflowID, name)
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
