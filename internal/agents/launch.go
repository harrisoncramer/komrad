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
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tasks"
	"komrad/lib/clock"
	"komrad/lib/xptr"
	"komrad/lib/xstr"
)

// errTaskMadeAndJoined is a launch naming a task to join and describing one to make.
var errTaskMadeAndJoined = fail.InvalidArgument("an agent either joins a task or has one made for it, not both")

type NewLaunchServiceParams struct {
	Tmux               paneController
	Agents             *agentstore.AgentStore
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
		workflows: params.Workflows,
		tx:        params.Tx,
		tasks:     params.Tasks,
		starter: &agentStarter{
			tmux:     params.Tmux,
			launcher: params.Launcher,
			scripts:  params.Scripts,
			agents:   params.Agents,
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
	HideTask         bool
	IdempotencyKey   string
	AdoptedBranch    string
	RepositoryURL    string
	Role             agentstore.Role
	Task             string
	TaskSlug         string
	TaskGoal         string
	// StandOn is the task in the workflow the new task stands on, by id or slug, empty for none.
	StandOn string
}

// NewAgent creates and launches a background agent, answering a key already spent on a launch
// with that agent and agenterr.ErrAlreadyLaunched.
func (s *LaunchService) NewAgent(ctx context.Context, params NewAgentParams) (*agentstore.Agent, error) {
	if params.IdempotencyKey != "" {
		key := idempotencyKey(ctx, params)
		existing, err := s.agents.GetByIdempotencyKey(ctx, key)
		switch {
		case err == nil:
			// Komrad writes the row before it opens the window, so a create that failed leaves the
			// key spent on an agent that never ran and a retry that gets a corpse.
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

	var stand *taskstore.Stand
	if params.StandOn != "" {
		if params.Task != "" {
			return nil, tasks.ErrStandJoined
		}

		planned, err := s.tasks.PlanStand(ctx, tasks.PlanStandParams{WorkflowID: params.WorkflowID, Parent: params.StandOn})
		if err != nil {
			return nil, err
		}
		stand = planned
	}

	var joined *taskstore.Task
	if params.Task != "" {
		found, err := s.findJoinedTask(ctx, params)
		if err != nil {
			return nil, err
		}
		joined = found

		params, err = s.joinParams(ctx, params, *found)
		if err != nil {
			return nil, err
		}
	}
	if params.Role == "" {
		params.Role = agentstore.RoleWriter
	}

	if err := requireRoomForAgents(ctx, s.agents, s.usage.MaxAgents, 1); err != nil {
		return nil, err
	}

	return s.launchAgent(ctx, launchAgentParams{request: params, spawner: spawner, joined: joined, stand: stand})
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
	}
}

// requireOwnWorkflow holds an orchestrator to the workflow it stands behind.
func requireOwnWorkflow(spawner agentstore.Agent, params NewAgentParams) error {
	if spawner.Role != agentstore.RoleOrchestrator || params.WorkflowID == spawner.WorkflowID {
		return nil
	}

	return fmt.Errorf("%w: %q stands behind workflow %q", agenterr.ErrOutsideWorkflow, spawner.Name, spawner.WorkflowID)
}

// findSpawnedBy picks the agent a new one answers to, the workflow's orchestrator for a writer
// starting a task of its own.
func (s *LaunchService) findSpawnedBy(ctx context.Context, spawner agentstore.Agent, params NewAgentParams) (*agentstore.Agent, error) {
	if spawner.Role != agentstore.RoleWriter || params.Task != "" {
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

// findJoinedTask resolves the live task a launch names, refusing one that also describes a task
// to make.
func (s *LaunchService) findJoinedTask(ctx context.Context, params NewAgentParams) (*taskstore.Task, error) {
	if params.TaskSlug != "" || params.TaskGoal != "" {
		return nil, errTaskMadeAndJoined
	}

	task, err := s.tasks.GetTaskForWorkflow(ctx, params.Task, params.WorkflowID)
	if err != nil {
		return nil, err
	}
	if task.ArchivedAt != nil {
		return nil, tasks.ErrArchived.Saying("task %q is archived", task.Slug)
	}

	return task, nil
}

// joinParams makes a launch onto a task that has a writer one of its reviewers, holding it to
// the checkout the task already has, and leaves the first agent on a task to become its writer.
func (s *LaunchService) joinParams(ctx context.Context, params NewAgentParams, task taskstore.Task) (NewAgentParams, error) {
	onTask, err := s.agents.ListOnTasks(ctx, []string{task.ID})
	if err != nil {
		return params, err
	}

	var writer *agentstore.Agent
	if i := slices.IndexFunc(onTask, isTaskWriter); i >= 0 {
		writer = &onTask[i]
	}
	if writer == nil && params.Role != agentstore.RoleReviewer {
		return params, nil
	}

	switch {
	case strings.TrimSpace(params.Dir) != "":
		return params, fmt.Errorf("%w: it runs in task %q's checkout", agenterr.ErrReviewerOwnsLaunch, task.Slug)
	case params.ProvisionEnabled != nil && *params.ProvisionEnabled:
		return params, fmt.Errorf("%w: it provisions nothing, since task %q's checkout is already there", agenterr.ErrReviewerOwnsLaunch, task.Slug)
	case params.ScheduleID != nil:
		return params, fmt.Errorf("%w: a schedule starts a task rather than joining one", agenterr.ErrReviewerOwnsLaunch)
	case task.Checkout.WorktreeRemoved:
		return params, tasks.ErrWorktreeRemoved.Saying("task %q's worktree was removed, so there is no checkout to review", task.Slug)
	}

	dir := reviewerDir(task, writer)
	if dir == "" {
		return params, fmt.Errorf("%w: task %q has none recorded", agenterr.ErrReviewerNeedsDirectory, task.Slug)
	}

	params.Dir = dir
	params.ProvisionEnabled = xptr.Ptr(false)
	params.Role = agentstore.RoleReviewer

	return params, nil
}

// reviewerDir is where a reviewer on a task runs: the task's checkout, or the directory its writer
// works in when the task has none of its own.
func reviewerDir(task taskstore.Task, writer *agentstore.Agent) string {
	if task.Checkout.IsUsable() {
		return task.Checkout.Cwd
	}
	if writer != nil {
		return writer.Cwd
	}

	return ""
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
		dir = wf.Cwd
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

	provisioner, deprovisioner := scriptrun.WorktreeScripts(wf, params.ProvisionEnabled)

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
		hooks:         hooksFor(wf, params.Role, params.ProvisionEnabled),
		workflow:      wf,
	}, nil
}

type launchFlagsParams struct {
	harnessID       string
	workflowHarness string
	agentFlags      string
	workflowFlags   string
}

// launchFlags holds what an agent launches with, the user's defaults for the CLI it runs ahead
// of the flags it received.
func (s *LaunchService) launchFlags(params launchFlagsParams) string {
	// A workflow's flags target the CLI it names, so komrad launches an agent running a different
	// one without them.
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

func (p *launchPlan) newAgent(params NewAgentParams, spawner *agentstore.Agent) (*agentstore.Agent, error) {
	id, err := store.NewID()
	if err != nil {
		return nil, err
	}

	var spawnedBy *string
	if spawner != nil {
		spawnedBy = &spawner.ID
	}

	return &agentstore.Agent{
		ID:               id,
		Name:             p.name,
		Cwd:              p.dir,
		Flags:            p.flags,
		ProvisionEnabled: params.ProvisionEnabled,
		Prompt:           params.Prompt,
		State:            agentstore.StateSettingUp,
		Harness:          p.harnessID,
		SessionID:        harness.MintSessionID(p.harness),
		WorkflowID:       params.WorkflowID,
		ScheduleID:       params.ScheduleID,
		IdempotencyKey:   nullable(params.IdempotencyKey),
		SpawnedByAgentID: spawnedBy,
		Role:             params.Role,
	}, nil
}

type launchAgentParams struct {
	request NewAgentParams
	spawner *agentstore.Agent
	joined  *taskstore.Task
	stand   *taskstore.Stand
}

func (s *LaunchService) launchAgent(ctx context.Context, params launchAgentParams) (*agentstore.Agent, error) {
	request, spawner := params.request, params.spawner
	plan, err := s.planLaunch(ctx, request)
	if err != nil {
		return nil, err
	}

	agent, err := plan.newAgent(request, spawner)
	if err != nil {
		return nil, err
	}
	agent.RequestedByAgentID = nullable(caller.From(ctx).AgentID)

	placed, err := s.createUnderCeiling(ctx, creation{
		agent:         agent,
		task:          params.joined,
		spawner:       spawner,
		title:         request.Title,
		taskSlug:      request.TaskSlug,
		taskGoal:      request.TaskGoal,
		adoptedBranch: request.AdoptedBranch,
		repositoryURL: request.RepositoryURL,
		provisioner:   plan.provisioner,
		hideTask:      request.HideTask,
		stand:         params.stand,
	})
	if err != nil {
		return nil, err
	}
	if placed.openTurn != nil {
		defer placed.openTurn.Done()
	}

	return s.startInPane(ctx, *agent, resolvedLaunch{
		harness:        plan.harness,
		provisioner:    plan.provisioner,
		deprovisioner:  plan.deprovisioner,
		hooks:          plan.hooks,
		defaultDir:     plan.dir,
		spawner:        spawner,
		claimsCheckout: isCheckoutClaimant(*agent, plan.provisioner),
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

	return scriptrun.HookRun{
		Scripts: c.scripts,
		Agent:   agent,
		Hooks:   c.resolved.hooks,
		Event:   workflowstore.HookAgentPreLaunch,
	}.RunInTurn(ctx)
}

// openTask runs the pre-open hooks of the task an agent opens, or waits on the agent opening it already.
func (c *createStart) openTask(ctx context.Context, agent agentstore.Agent) error {
	if c.resolved.task == nil {
		return nil
	}

	return c.service.tasks.RunPreOpen(ctx, tasks.RunPreOpenParams{
		Agent:    agent,
		Task:     c.buildOpenedTask(agent),
		Workflow: c.resolved.workflow,
		Turn:     c.resolved.openTurn,
	})
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
		OwnsCheckout: c.resolved.claimsCheckout,
		Task:         c.resolved.task,
	}
	if c.resolved.workflow != nil {
		launch.WorkflowContext = c.resolved.workflow.Context
	}
	if c.resolved.spawner != nil {
		launch.SpawnedByAgentID = c.resolved.spawner.ID
		launch.SpawnedByName = c.resolved.spawner.Name
	}

	return paneLaunch{
		harness: c.resolved.harness,
		params:  launch,
		verb:    "launch",
	}
}

func (c *createStart) finish(ctx context.Context, agent agentstore.Agent) error {
	scriptrun.HookRun{
		Scripts: c.scripts,
		Agent:   agent,
		Hooks:   c.resolved.hooks,
		Event:   workflowstore.HookAgentPostLaunch,
	}.Start(ctx)
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
	failAs(ctx, failAsParams{
		agents: s.agents,
		agent:  agent,
		state:  agentstore.StateFailedToCreate,
		verb:   "create",
		cause:  cause,
	})
}

// awaitDeprovision runs a deprovisioner and waits for it, so a create that failed gives back
// the directory it provisioned before the row is closed out.
func awaitDeprovision(ctx context.Context, deprovision agentScriptRunner, agent agentstore.Agent, name string) {
	finished := make(chan error, 1)
	runDeprovision(ctx, runDeprovisionParams{
		deprovision: deprovision,
		agent:       agent,
		name:        name,
		done: func(deprovisionErr error) {
			finished <- deprovisionErr
		},
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
	title    string
	taskSlug string
	taskGoal string
	// adoptedBranch is the branch the agent's checkout is put on rather than one komrad makes.
	adoptedBranch string
	repositoryURL string
	provisioner   string
	hideTask      bool
	stand         *taskstore.Stand
}

// isCheckoutClaimant reports whether an agent's directory becomes its task's checkout, which only a
// writer that provisions does.
func isCheckoutClaimant(agent agentstore.Agent, provisioner string) bool {
	return agent.TaskID != nil && agent.Role == agentstore.RoleWriter && provisioner != ""
}

// placement is the task an agent was created on and the place in its hook queue the agent holds
// when it opens it.
type placement struct {
	task     *taskstore.Task
	openTurn *scriptrun.HookTurn
}

// createUnderCeiling writes the agent, and the task it works on when it needs one of its own,
// returning where it was placed.
func (s *LaunchService) createUnderCeiling(ctx context.Context, c creation) (*placement, error) {
	var opened *tasks.Opened
	err := s.tx.InTx(ctx, func(db store.DBTX) error {
		agentStore := agentstore.NewAgentStore(db)

		if err := claimRoomForAgents(ctx, agentStore, s.usage.MaxAgents, 1); err != nil {
			return err
		}
		if c.agent.Role == agentstore.RoleWriter {
			if err := claimRoomForTask(ctx, agentStore, s.usage.Orchestrator.MaxTasks, c.spawner); err != nil {
				return err
			}
		}

		var err error
		opened, err = s.tasks.Open(ctx, db, tasks.OpenParams{
			Agent:  *c.agent,
			Joined: c.task,
			Title:  c.title,
			Slug:   c.taskSlug,
			Goal:   c.taskGoal,
			Hidden: c.hideTask,
			Stand:  c.stand,
		})
		if err != nil {
			return err
		}
		if opened.Task != nil {
			c.agent.TaskID = &opened.Task.ID
		}

		if c.agent.Role == agentstore.RoleReviewer {
			if err := claimRoomForReviewer(ctx, agentStore, s.usage.Task.MaxReviewers, *c.agent.TaskID); err != nil {
				return err
			}
		}
		if err := agentStore.Create(ctx, *c.agent); err != nil {
			return fmt.Errorf("failed to record agent %q: %w", c.agent.Name, err)
		}
		if !isCheckoutClaimant(*c.agent, c.provisioner) {
			return nil
		}

		return tasks.ClaimCheckout(ctx, db, tasks.ClaimCheckoutParams{
			TaskID:        opened.Task.ID,
			AgentName:     c.agent.Name,
			AdoptedBranch: c.adoptedBranch,
			RepositoryURL: c.repositoryURL,
		})
	})
	if err != nil {
		if opened != nil && opened.Turn != nil {
			opened.Turn.Done()
		}

		return nil, err
	}

	return &placement{
		task:     opened.Task,
		openTurn: opened.Turn,
	}, nil
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
