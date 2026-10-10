package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/agenterr"
	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/harness"
	"komrad/internal/logging"
	"komrad/internal/scriptrun"
	"komrad/internal/scripts"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tasks"
	"komrad/lib/clock"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	"komrad/lib/xstr"
)

// errTaskMadeAndJoined is a launch naming a task to join and describing one to make.
var errTaskMadeAndJoined = fail.InvalidArgument("an agent either joins a task or has one made for it, not both")

// errTaskHasCheckout is a launch that would make a checkout for a task that already has one.
var errTaskHasCheckout = fail.Precondition("this task already has a checkout, so launch a child of the agent working in it or give this one a task of its own")

type NewLaunchServiceParams struct {
	Tmux               paneController
	Agents             *agentstore.AgentStore
	Reviews            *reviewstore.ReviewStore
	Workflows          *workflowstore.WorkflowStore
	Tx                 txRunner
	Tasks              *tasks.TaskService
	Launcher           *Launcher
	Harnesses          harnessGetter
	Scripts            agentScriptRunner
	UserDefaultHarness string
	UserDefaultFlags   map[string]string
	Usage              config.Usage
	Sleeper            clock.Sleeper
}

type LaunchService struct {
	tmux               paneController
	agents             *agentstore.AgentStore
	reviews            *reviewstore.ReviewStore
	workflows          *workflowstore.WorkflowStore
	tx                 txRunner
	tasks              *tasks.TaskService
	starter            *agentStarter
	harnesses          harnessGetter
	userDefaultHarness string
	userDefaultFlags   map[string]string
	usage              config.Usage
	sleeper            clock.Sleeper
}

// NewLaunchService builds the service from its dependencies.
func NewLaunchService(params NewLaunchServiceParams) *LaunchService {
	sleeper := params.Sleeper
	if sleeper == nil {
		sleeper = clock.Wall{}
	}

	return &LaunchService{
		tmux:      params.Tmux,
		agents:    params.Agents,
		reviews:   params.Reviews,
		workflows: params.Workflows,
		tx:        params.Tx,
		tasks:     params.Tasks,
		starter: &agentStarter{
			tmux:     params.Tmux,
			launcher: params.Launcher,
			scripts:  params.Scripts,
		},
		harnesses:          params.Harnesses,
		userDefaultHarness: params.UserDefaultHarness,
		userDefaultFlags:   params.UserDefaultFlags,
		usage:              params.Usage,
		sleeper:            sleeper,
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
	RepositoryURL    string
	ParentAgentID    string
	Role             agentstore.Role
	Task             string
	TaskSlug         string
	TaskGoal         string
}

// NewAgent creates and launches a background agent, answering a key already spent on a launch
// with that agent and agenterr.ErrAlreadyLaunched.
func (s *LaunchService) NewAgent(ctx context.Context, params NewAgentParams) (*agentstore.Agent, error) {
	if params.IdempotencyKey != "" {
		key := idempotencyKey(ctx, params)
		existing, err := s.agents.GetByIdempotencyKey(ctx, key)
		switch {
		case err == nil:
			// The row is written before the window is opened, so a create that failed leaves the
			// key spent on an agent that never ran and a retry answered with a corpse.
			released, err := s.releaseFailedLaunch(ctx, *existing, key)
			if err != nil {
				return nil, err
			}
			if !released {
				return existing, agenterr.ErrAlreadyLaunched
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
		if err := requireOwnWorkflow(*found, params); err != nil {
			return nil, err
		}

		spawner, err = s.findSpawnedBy(ctx, *found, params)
		if err != nil {
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

	if err := requireRoomForAgents(ctx, s.agents, s.usage.MaxAgents, 1); err != nil {
		return nil, err
	}

	return s.launchAgent(ctx, params, spawner, parent)
}

// releaseFailedLaunch frees an idempotency key held by a create that failed, so the next
// attempt launches.
func (s *LaunchService) releaseFailedLaunch(ctx context.Context, existing agentstore.Agent, key agentstore.IdempotencyKey) (bool, error) {
	// A failed cleanup belongs to an agent that ran, so only a failed create launched nothing.
	if existing.State != agentstore.StateFailedToCreate {
		return false, nil
	}

	// The row is left where it stands rather than relaunched into, its id naming the worktree and
	// branch the deprovisioner is still tearing down.
	return s.agents.ReleaseIdempotencyKey(ctx, key)
}

func idempotencyKey(ctx context.Context, params NewAgentParams) agentstore.IdempotencyKey {
	return agentstore.IdempotencyKey{
		Key:                params.IdempotencyKey,
		RequestedByAgentID: nullable(caller.From(ctx).AgentID),
		ParentAgentID:      nullable(params.ParentAgentID),
	}
}

// requireOwnWorkflow holds an orchestrator to the workflow it stands behind.
func requireOwnWorkflow(spawner agentstore.Agent, params NewAgentParams) error {
	if spawner.Role != agentstore.RoleOrchestrator || params.WorkflowID == spawner.WorkflowID {
		return nil
	}

	return fmt.Errorf("%w: %q stands behind workflow %q", agenterr.ErrOutsideWorkflow, spawner.Name, spawner.WorkflowID)
}

// findSpawnedBy picks the agent a new one answers to, the workflow's orchestrator for an implementer's standalone spawn.
func (s *LaunchService) findSpawnedBy(ctx context.Context, spawner agentstore.Agent, params NewAgentParams) (*agentstore.Agent, error) {
	if spawner.Role != agentstore.RoleImplementer || params.ParentAgentID != "" {
		return &spawner, nil
	}

	orchestrator, err := s.agents.GetWorkflowOrchestrator(ctx, spawner.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return &spawner, nil
	}
	if err != nil {
		return nil, err
	}
	if orchestrator.ArchivedAt != nil {
		return &spawner, nil
	}

	return orchestrator, nil
}

func (s *LaunchService) findSpawner(ctx context.Context, id string) (*agentstore.Agent, error) {
	spawner, err := s.agents.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", agenterr.ErrSpawnerUnknown, id)
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
		return nil, fmt.Errorf("%w: %q", agenterr.ErrParentNotFound, params.ParentAgentID)
	}
	if err != nil {
		return nil, err
	}
	if parent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q", agenterr.ErrAgentArchived, parent.Name)
	}
	if parent.ParentAgentID != nil {
		return nil, fmt.Errorf("%w: %q is itself attached to another agent", agenterr.ErrParentIsChild, parent.Name)
	}
	if parent.Role == agentstore.RoleOrchestrator {
		return nil, fmt.Errorf(
			"%w: %q is drawn on its workflow's own row, so a child of it would have no row anywhere; spawn an ordinary agent into the workflow with provision_enabled false instead",
			agenterr.ErrParentIsOrchestrator, parent.Name)
	}

	// Ahead of what the request asked for, which puts the parent's missing directory in the
	// refusal rather than a conflict with an empty one.
	if parent.Cwd == "" {
		return nil, fmt.Errorf("%w: %q has none recorded", agenterr.ErrChildNeedsParentDirectory, parent.Name)
	}

	switch {
	case strings.TrimSpace(params.Dir) != "":
		return nil, fmt.Errorf("%w: it runs in %q, its parent's own checkout", agenterr.ErrChildOwnsLaunch, parent.Cwd)
	case params.ProvisionEnabled != nil && *params.ProvisionEnabled:
		return nil, fmt.Errorf("%w: it provisions nothing, since the checkout is already there", agenterr.ErrChildOwnsLaunch)
	case params.WorkflowID != parent.WorkflowID:
		return nil, fmt.Errorf("%w: it is filed in its parent's workflow %q", agenterr.ErrChildOwnsLaunch, parent.WorkflowID)
	case params.ScheduleID != nil:
		return nil, fmt.Errorf("%w: a schedule launches into a workflow rather than onto an agent", agenterr.ErrChildOwnsLaunch)
	case params.Task != "" || params.TaskSlug != "" || params.TaskGoal != "":
		return nil, fmt.Errorf("%w: it works on its parent's task", agenterr.ErrChildOwnsLaunch)
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
	name          string
	harness       harness.Harness
	harnessID     string
	dir           string
	flags         string
	provisioner   string
	deprovisioner string
	hooks         []workflowstore.Hook
	workflow      *workflowstore.Workflow
}

func (s *LaunchService) planLaunch(ctx context.Context, params NewAgentParams) (*launchPlan, error) {
	wf, err := s.workflows.Get(ctx, params.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, agenterr.ErrWorkflowNotFound.Saying("no workflow with id %q", params.WorkflowID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", params.WorkflowID, err)
	}

	workflowHarness, workflowFlags := s.roleDefaults(wf, params.Role)

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

	provisioner, deprovisioner := worktreeScripts(wf, params.ProvisionEnabled)

	return &launchPlan{
		name:      s.uniqueAgentName(ctx, params.WorkflowID, params.Title, params.Prompt),
		harness:   hn,
		harnessID: harnessID,
		dir:       dir,
		flags: s.launchFlags(launchFlagsParams{
			harnessID:       harnessID,
			workflowHarness: workflowHarness,
			agentFlags:      params.Flags,
			workflowFlags:   workflowFlags,
		}),
		provisioner:   provisioner,
		deprovisioner: deprovisioner,
		hooks:         hooksFor(wf, params.ParentAgentID != "", params.ProvisionEnabled),
		workflow:      wf,
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

	defaults := strings.TrimSpace(s.userDefaultFlags[params.harnessID])
	if strings.Contains(" "+flags+" ", " "+defaults+" ") {
		return flags
	}

	return strings.TrimSpace(defaults + " " + flags)
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

// roleDefaults is the CLI and flags a workflow gives an agent in role that names none of its own.
func (s *LaunchService) roleDefaults(wf *workflowstore.Workflow, role agentstore.Role) (string, string) {
	harnessID := s.workflowHarness(wf)
	if wf == nil {
		return harnessID, ""
	}
	if role != agentstore.RoleReviewer {
		return harnessID, wf.Flags
	}

	reviewerHarness := strings.TrimSpace(wf.ReviewerHarness)
	if reviewerHarness == "" {
		reviewerHarness = harnessID
	}
	if flags := strings.TrimSpace(wf.ReviewerFlags); flags != "" || reviewerHarness != harnessID {
		return reviewerHarness, flags
	}

	return reviewerHarness, wf.Flags
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
		Role:             params.Role,
	}
}

func (s *LaunchService) launchAgent(ctx context.Context, params NewAgentParams, spawner, parent *agentstore.Agent) (result *agentstore.Agent, launchErr error) {
	if params.Task != "" && (params.TaskSlug != "" || params.TaskGoal != "") {
		return nil, errTaskMadeAndJoined
	}

	var joined *taskstore.Task
	if params.Task != "" {
		found, err := s.tasks.GetTaskForWorkflow(ctx, params.Task, params.WorkflowID)
		if err != nil {
			return nil, err
		}
		joined = found
	}

	plan, err := s.planLaunch(ctx, params)
	if err != nil {
		return nil, err
	}

	id, err := store.NewID()
	if err != nil {
		return nil, err
	}
	sessionID := harness.MintSessionID(plan.harness)
	agent := plan.agent(params, id, sessionID, plan.dir, "", spawner)
	agent.RequestedByAgentID = nullable(caller.From(ctx).AgentID)
	agent.State = agentstore.StateSettingUp

	placed, err := s.createUnderCeiling(ctx, creation{
		agent:         &agent,
		task:          joined,
		spawner:       spawner,
		parent:        parent,
		title:         params.Title,
		taskSlug:      params.TaskSlug,
		taskGoal:      params.TaskGoal,
		adoptedBranch: params.AdoptedBranch,
		repositoryURL: params.RepositoryURL,
		provisioner:   plan.provisioner,
	})
	if err != nil {
		return nil, err
	}
	if placed.openTurn != nil {
		defer placed.openTurn.Done()
	}

	return s.startInPane(ctx, agent, resolvedLaunch{
		harness:        plan.harness,
		provisioner:    plan.provisioner,
		deprovisioner:  plan.deprovisioner,
		hooks:          plan.hooks,
		defaultDir:     plan.dir,
		spawner:        spawner,
		parent:         parent,
		claimsCheckout: isCheckoutClaimant(agent, plan.provisioner),
		workflow:       plan.workflow,
		task:           placed.task,
		openTurn:       placed.openTurn,
	})
}

type resolvedLaunch struct {
	harness       harness.Harness
	provisioner   string
	deprovisioner string
	hooks         []workflowstore.Hook
	defaultDir    string
	spawner       *agentstore.Agent
	parent        *agentstore.Agent
	// claimsCheckout is whether the directory the agent starts in becomes its task's checkout.
	claimsCheckout bool
	workflow       *workflowstore.Workflow
	task           *taskstore.Task
	// openTurn is the place in its task's hook queue an agent taking the task from no live agent to
	// one holds, nil for any other agent.
	openTurn *scriptrun.HookTurn
}

func (s *LaunchService) startInPane(ctx context.Context, agent agentstore.Agent, resolved resolvedLaunch) (*agentstore.Agent, error) {
	if err := s.starter.start(ctx, agent, &createStart{
		service:  s,
		scripts:  s.starter.scripts,
		resolved: resolved,
	}); err != nil {
		return nil, err
	}

	created, err := s.agents.GetByID(ctx, agent.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to read back agent %q: %w", agent.Name, err)
	}

	return created, nil
}

type createStart struct {
	service     *LaunchService
	scripts     agentScriptRunner
	resolved    resolvedLaunch
	provisioned bool
}

func (c *createStart) runSetup(ctx context.Context, agent agentstore.Agent) (string, error) {
	if c.scripts == nil || c.resolved.provisioner == "" {
		return "", nil
	}

	named, err := c.scripts.RunProvision(ctx, agent, c.resolved.provisioner)
	if err != nil {
		return "", err
	}
	c.provisioned = true

	return named, nil
}

func (c *createStart) getDefaultDir(agentstore.Agent) string {
	return c.resolved.defaultDir
}

func (c *createStart) runPreHooks(ctx context.Context, agent agentstore.Agent) error {
	if err := c.openTask(ctx, agent); err != nil {
		return err
	}

	return runPreHooks(ctx, hookRunParams{
		scripts: c.scripts,
		agent:   agent,
		hooks:   c.resolved.hooks,
		event:   workflowstore.HookAgentPreLaunch,
	})
}

// openTask runs the pre-open hooks of the task an agent opens, and otherwise waits for an agent opening it
// already, opening it in that agent's place when it failed.
func (c *createStart) openTask(ctx context.Context, agent agentstore.Agent) error {
	if c.scripts == nil || agent.TaskID == nil || c.resolved.task == nil {
		return nil
	}

	turn := c.resolved.openTurn
	if turn == nil {
		if err := c.scripts.AwaitHooks(ctx, *agent.TaskID); err != nil {
			return err
		}

		live, err := c.service.agents.ListLiveOnTask(ctx, *agent.TaskID)
		if err != nil {
			return err
		}
		if !isOpening(agent, live) {
			return nil
		}

		turn = c.scripts.QueueHooks(*agent.TaskID)
		defer turn.Done()
	}

	if err := turn.Wait(ctx); err != nil {
		return err
	}

	return runHooks(ctx, taskHookParams{
		scripts:  c.scripts,
		workflow: c.resolved.workflow,
		task:     c.buildOpenedTask(agent),
		actor:    &agent,
		event:    workflowstore.HookTaskPreOpen,
	}.toHookRun())
}

// buildOpenedTask builds the task the agent opens, carrying the checkout it was just given before the row records it.
func (c *createStart) buildOpenedTask(agent agentstore.Agent) taskstore.Task {
	task := *c.resolved.task
	if c.resolved.claimsCheckout {
		task.Checkout.Cwd = agent.Cwd
	}

	return task
}

func (c *createStart) record(ctx context.Context, agent *agentstore.Agent, pane string) error {
	agent.Pane = pane
	agent.State = agentstore.StateIdle
	written, err := c.service.agents.UpdateIfLive(ctx, *agent)
	if err != nil {
		return fmt.Errorf("failed to record agent %q: %w", agent.Name, err)
	}
	if !written {
		return fmt.Errorf("%w: %q was archived or deleted while it was being created", agenterr.ErrAgentArchived, agent.Name)
	}
	if c.resolved.claimsCheckout {
		if err := c.service.tasks.SetCheckoutDir(ctx, *agent.TaskID, agent.Cwd); err != nil {
			return fmt.Errorf("failed to record the checkout of agent %q on its task: %w", agent.Name, err)
		}
	}

	return nil
}

func (c *createStart) planLaunch(agent agentstore.Agent) paneLaunch {
	launch := launchParams{
		Pane:         agent.Pane,
		Dir:          agent.Cwd,
		Flags:        agent.Flags,
		Prompt:       agent.Prompt,
		SessionID:    agent.SessionID,
		AgentID:      agent.ID,
		Role:         agent.Role,
		OwnsCheckout: c.resolved.provisioner != "",
		Task:         c.resolved.task,
	}
	if c.resolved.workflow != nil {
		launch.WorkflowContext = c.resolved.workflow.Context
	}
	if c.resolved.spawner != nil {
		launch.SpawnedByAgentID = c.resolved.spawner.ID
		launch.SpawnedByName = c.resolved.spawner.Name
	}
	if c.resolved.parent != nil {
		launch.ParentAgentID = c.resolved.parent.ID
		launch.ParentName = c.resolved.parent.Name
	}

	return paneLaunch{
		harness: c.resolved.harness,
		params:  launch,
		verb:    "launch",
	}
}

func (c *createStart) finish(ctx context.Context, agent agentstore.Agent) error {
	startHooks(ctx, hookRunParams{
		scripts: c.scripts,
		agent:   agent,
		hooks:   c.resolved.hooks,
		event:   workflowstore.HookAgentPostLaunch,
	})
	c.service.watchStartup(ctx, agent)

	return nil
}

func (c *createStart) rollBack(ctx context.Context, agent agentstore.Agent, cause error) {
	if c.provisioned {
		awaitDeprovision(ctx, c.scripts, agent, c.resolved.deprovisioner)
	}
	if c.resolved.claimsCheckout {
		c.service.releaseCheckout(ctx, agent)
	}
	c.service.failCreate(ctx, agent, cause)
}

// releaseCheckout gives up the claim a failed create made on its task's checkout, logging any failure.
func (s *LaunchService) releaseCheckout(ctx context.Context, agent agentstore.Agent) {
	if err := s.tasks.ReleaseCheckout(ctx, *agent.TaskID); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to release the checkout claim of a failed create",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.TaskID, *agent.TaskID),
			}, logging.Err(err)...)...)
	}
}

// teardownTimeout stands in for the caller's own deadline once that no longer applies.
const teardownTimeout = 30 * time.Second

func (s *LaunchService) failCreate(ctx context.Context, agent agentstore.Agent, cause error) {
	agent.Pane = ""
	failAs(ctx, s.agents, agent, agentstore.StateFailedToCreate, "create", cause)
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

// creation is an agent row about to be written, the task it joins and the agents it is counted
// against.
type creation struct {
	agent    *agentstore.Agent
	task     *taskstore.Task
	spawner  *agentstore.Agent
	parent   *agentstore.Agent
	title    string
	taskSlug string
	taskGoal string
	// adoptedBranch is the branch the agent's checkout is put on rather than one komrad makes.
	adoptedBranch string
	repositoryURL string
	provisioner   string
}

// needsOwnTask is whether the agent is given a task of its own as it is written, which a child
// sharing its parent's and an orchestrator are not.
func (c creation) needsOwnTask() bool {
	return c.task == nil && c.parent == nil && c.agent.Role != agentstore.RoleOrchestrator
}

// isCheckoutClaimant reports whether an agent's directory becomes its task's checkout, which only a
// top-level agent that provisions does.
func isCheckoutClaimant(agent agentstore.Agent, provisioner string) bool {
	return agent.TaskID != nil && agent.ParentAgentID == nil && agent.Role != agentstore.RoleOrchestrator && provisioner != ""
}

// placement is the task an agent was created on, and the place in its hook queue the agent holds when it opens it.
type placement struct {
	task     *taskstore.Task
	openTurn *scriptrun.HookTurn
}

// createUnderCeiling writes the agent, and the task it works on when it needs one of its own,
// returning where it was placed.
func (s *LaunchService) createUnderCeiling(ctx context.Context, c creation) (*placement, error) {
	task := c.task
	var openTurn *scriptrun.HookTurn
	err := s.tx.InTx(ctx, func(db store.DBTX) error {
		agentStore := agentstore.NewAgentStore(db)
		taskStore := taskstore.NewTaskStore(db)

		if err := claimRoomForAgents(ctx, agentStore, s.usage.MaxAgents, 1); err != nil {
			return err
		}
		if err := claimRoomForChildren(ctx, agentStore, s.usage.Implementer.MaxChildren, c.parent); err != nil {
			return err
		}
		if c.parent == nil {
			if err := claimRoomForSpawned(ctx, agentStore, s.usage.Orchestrator.MaxChildren, c.spawner); err != nil {
				return err
			}
		}

		if c.needsOwnTask() {
			created, err := tasks.CreateForAgent(ctx, taskStore, tasks.CreateForAgentParams{
				WorkflowID: c.agent.WorkflowID,
				Name:       c.agent.Name,
				Title:      c.title,
				Prompt:     c.agent.Prompt,
				Slug:       c.taskSlug,
				Goal:       c.taskGoal,
			})
			if err != nil {
				return fmt.Errorf("failed to make a task for agent %q: %w", c.agent.Name, err)
			}
			task = created
		}
		if task == nil && c.parent != nil && c.parent.TaskID != nil {
			found, err := taskStore.Get(ctx, *c.parent.TaskID)
			if err != nil {
				return fmt.Errorf("failed to read the task of agent %q's parent: %w", c.agent.Name, err)
			}
			task = found
		}
		if task != nil {
			c.agent.TaskID = &task.ID
		}
		if c.agent.TaskID != nil {
			if err := taskStore.LockUndeleted(ctx, *c.agent.TaskID); err != nil {
				return err
			}

			live, err := agentStore.ListLiveOnTask(ctx, *c.agent.TaskID)
			if err != nil {
				return err
			}
			openTurn = s.queueOpening(*c.agent, live)
		}
		if err := agentStore.Create(ctx, *c.agent); err != nil {
			return fmt.Errorf("failed to record agent %q: %w", c.agent.Name, err)
		}
		if isCheckoutClaimant(*c.agent, c.provisioner) {
			if err := claimCheckout(ctx, taskStore, c, task.ID); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		if openTurn != nil {
			openTurn.Done()
		}

		return nil, err
	}

	return &placement{
		task:     task,
		openTurn: openTurn,
	}, nil
}

// queueOpening takes a place in its task's hook queue for an agent opening the task, nil when it does not open it.
func (s *LaunchService) queueOpening(agent agentstore.Agent, live []agentstore.Agent) *scriptrun.HookTurn {
	if s.starter.scripts == nil || !isOpening(agent, live) {
		return nil
	}

	return s.starter.scripts.QueueHooks(*agent.TaskID)
}

// isOpening reports whether an agent starting on a task opens it, which it does when no other agent on the task is live.
func isOpening(agent agentstore.Agent, live []agentstore.Agent) bool {
	if agent.TaskID == nil || agent.ParentAgentID != nil {
		return false
	}

	return !slices.ContainsFunc(live, func(other agentstore.Agent) bool {
		return other.ID != agent.ID && other.State != agentstore.StateFailedToCreate
	})
}

// claimCheckout records on a task how its checkout is to be made, ahead of the directory the
// launch then provisions.
func claimCheckout(ctx context.Context, tasks *taskstore.TaskStore, c creation, taskID string) error {
	claimed, err := tasks.ClaimCheckout(ctx, taskstore.ClaimCheckoutParams{
		ID:            taskID,
		AdoptedBranch: c.adoptedBranch,
		RepositoryURL: c.repositoryURL,
	})
	if err != nil {
		return fmt.Errorf("failed to claim the checkout of task %q for agent %q: %w", taskID, c.agent.Name, err)
	}
	if !claimed {
		return errTaskHasCheckout
	}

	return nil
}

func claimRoomForAgents(ctx context.Context, agents *agentstore.AgentStore, maxAgents, want int) error {
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
func claimRoomForChildren(ctx context.Context, agents *agentstore.AgentStore, maxChildren int, parent *agentstore.Agent) error {
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
func claimRoomForRestoredChildren(ctx context.Context, agents *agentstore.AgentStore, maxChildren int, restoring []agentstore.Agent) error {
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

func claimRoomForSpawned(ctx context.Context, agents *agentstore.AgentStore, maxSpawned int, spawner *agentstore.Agent) error {
	if spawner == nil || spawner.Role != agentstore.RoleOrchestrator || maxSpawned <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForSpawned(ctx, agents, maxSpawned, []parentRoom{{
		id:   spawner.ID,
		name: spawner.Name,
		want: 1,
	}})
}

func claimRoomForRestoredSpawned(ctx context.Context, agents *agentstore.AgentStore, maxSpawned int, restoring []agentstore.Agent) error {
	if maxSpawned <= 0 {
		return nil
	}

	want := make(map[string]int, len(restoring))
	var spawnerIDs []string
	for _, agent := range restoring {
		if agent.SpawnedByAgentID == nil || agent.ParentAgentID != nil {
			continue
		}
		if want[*agent.SpawnedByAgentID] == 0 {
			spawnerIDs = append(spawnerIDs, *agent.SpawnedByAgentID)
		}
		want[*agent.SpawnedByAgentID]++
	}
	if len(spawnerIDs) == 0 {
		return nil
	}

	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	spawners, err := agents.ListByID(ctx, spawnerIDs)
	if err != nil {
		return fmt.Errorf("failed to look up %d agents that spawned the restoring ones: %w", len(spawnerIDs), err)
	}

	orchestrators := xslice.Filter(spawners, func(spawner agentstore.Agent) bool {
		return spawner.Role == agentstore.RoleOrchestrator
	})

	return requireRoomForSpawned(ctx, agents, maxSpawned, xslice.Map(orchestrators, func(spawner agentstore.Agent) parentRoom {
		return parentRoom{
			id:   spawner.ID,
			name: spawner.Name,
			want: want[spawner.ID],
		}
	}))
}

func requireRoomForSpawned(ctx context.Context, agents *agentstore.AgentStore, maxSpawned int, wanted []parentRoom) error {
	for _, spawner := range wanted {
		held, err := agents.CountLiveSpawned(ctx, spawner.id)
		if err != nil {
			return fmt.Errorf("failed to count the agents spawned by %q: %w", spawner.name, err)
		}
		if held+spawner.want > maxSpawned {
			return fmt.Errorf("%w: %q may run %d agents it spawned at once and this would make %d",
				agenterr.ErrTooManySpawned, spawner.name, maxSpawned, held+spawner.want)
		}
	}

	return nil
}

// parentRoom is one parent and how many children a call wants to put under it.
type parentRoom struct {
	id   string
	name string
	want int
}

func requireRoomForChildren(ctx context.Context, agents *agentstore.AgentStore, maxChildren int, wanted []parentRoom) error {
	for _, parent := range wanted {
		held, err := agents.CountLiveChildren(ctx, parent.id)
		if err != nil {
			return fmt.Errorf("failed to count the children of agent %q: %w", parent.name, err)
		}
		if held+parent.want > maxChildren {
			return fmt.Errorf("%w: %q may run %d at once and this would make %d, and they share one checkout",
				agenterr.ErrTooManyChildren, parent.name, maxChildren, held+parent.want)
		}
	}

	return nil
}

func requireRoomForAgents(ctx context.Context, agents *agentstore.AgentStore, maxAgents, want int) error {
	if maxAgents <= 0 || want <= 0 {
		return nil
	}

	live, err := agents.CountLive(ctx)
	if err != nil {
		return fmt.Errorf("failed to count the agents already running: %w", err)
	}
	if live+int64(want) > int64(maxAgents) {
		return fmt.Errorf("%w: max number of concurrent agents reached (%d of %d), please archive one to continue", agenterr.ErrTooManyAgents, live, maxAgents)
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
