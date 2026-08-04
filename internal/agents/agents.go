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

// paneOwnerGetter reads which agent every live pane is stamped with, so a whole
// listing's liveness comes off one tmux call.
type paneOwnerGetter interface {
	GetPaneOwners() (map[string]string, error)
}

// paneKiller is the tmux stopping a set of agents needs. The owner stamps say whose
// pane is whose, and the kill takes it down.
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

// harnessGetter resolves the CLI an agent is recorded against, which is what knows how to
// launch it, read its frames, and find its log.
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

// agentClaimer counts the live agents and can hold that count still, which is what a
// caller about to write under the ceiling needs.
type agentClaimer interface {
	agentCounter
	LockSlots(ctx context.Context) error
}

type agentNamer interface {
	ActiveNameExists(ctx context.Context, workflowID, name string) (bool, error)
}

// AgentService owns an agent's lifecycle, coordinating the store with tmux and
// delegating the command line to the launcher.
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

// IsAgentRunning reports whether an agent's CLI is still up, which is true only of a
// pane stamped with that agent's id. tmux reissues pane ids from %0 after a server
// restart, so the stored pane id alone routinely names somebody else's pane.
func (s *AgentService) IsAgentRunning(agent agentstore.Agent) bool {
	return isPaneOwnedBy(s.tmux.GetPaneOwner(agent.Pane), agent.ID)
}

// GetRunningAgents answers liveness for a whole listing off one tmux call, keyed by agent
// id. A tmux that will not answer reports every agent as not running, which is what
// asking per agent already does when the server is unreachable.
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
	Client string
	Title  string
	Dir    string
	Flags  string
	Prompt string
	// Harness names the CLI this agent runs. Empty takes the workflow's default, and
	// then the configured one. An id nothing implements is refused.
	Harness string
	// WorkflowID is required. It supplies the directory and flags when Dir and Flags
	// are left empty, and the shared context appended to the agent's system prompt.
	WorkflowID string
	// ScheduleID records the schedule that launched this agent, nil for one a
	// person started. Provenance only.
	ScheduleID *string
	// Hidden starts the agent out of the panel's list, so a noisy recurring schedule
	// can stay armed without filling it. The agent still runs.
	Hidden bool
	// IdempotencyKey makes the create repeatable: a second call carrying a key an agent
	// already holds returns that agent untouched. Empty makes no claim.
	IdempotencyKey string
}

// NewAgent creates a background agent, records it so it lists immediately, and
// launches its CLI with the default plus per-agent flags and the opening prompt. It
// does not switch the client to the new agent, and returns the stored record so the
// caller learns the generated name and id.
func (s *AgentService) NewAgent(ctx context.Context, p NewAgentParams) (*agentstore.Agent, error) {
	// The key is checked before anything is opened or written, so a retry costs a
	// lookup rather than a tmux window that has to be cleaned up after.
	if p.IdempotencyKey != "" {
		existing, err := s.agents.GetByIdempotencyKey(ctx, p.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}

	// After the key, since a retry that finds its agent creates nothing, and before
	// anything is opened, so a refusal leaves no window, row, or setup action behind.
	// Creating an agent is a tool a model can call in a loop and each one is a real CLI
	// in a real pane. Holding the count still is the claim at the insert below.
	if err := requireRoomForAgents(ctx, s.agents, s.maxAgents, 1); err != nil {
		return nil, err
	}

	plan, err := s.planLaunch(ctx, p)
	if err != nil {
		return nil, err
	}

	// The id is minted here rather than by the insert below, because the pane has to be
	// stamped with it before the row exists. The reconciler reads an unstamped pane as
	// belonging to nobody and clears it out from under an agent about to launch in it.
	id, err := store.NewID()
	if err != nil {
		return nil, err
	}

	// The conversation is named before it exists, so restoring the agent can ask the
	// harness to reopen that same one. A harness that will not be told a name returns
	// none, leaving the id to be discovered after the launch.
	sessionID := plan.harness.MintSessionID()

	// Setup runs before the window opens, because where it opens is one of the things
	// setup decides. A refusal here leaves nothing to take back out.
	planned := plan.agent(p, id, sessionID)

	named, err := s.runSetup(ctx, planned, plan.setupAction)
	if err != nil {
		return nil, err
	}

	// Armed the moment the action has run, because from here on it may have built
	// something and everything below can still refuse, and a worktree no row names is
	// never reached again. It fires on the planned agent, since most of what can fail
	// below fails before the row exists.
	launched := false
	defer func() {
		if !launched {
			s.runCleanup(ctx, planned, plan.cleanupAction)
		}
	}()

	// Settled after the unwind is armed, so an action that built something and then named
	// somewhere unusable is still cleaned up after. That cleanup is told the planned
	// directory, since the name it was given is the one value known to be wrong.
	dir, err := s.setupDir(plan.dir, named)
	if err != nil {
		return nil, err
	}

	planned.Cwd = dir

	pane, err := s.tmux.NewWindow(ManagerSession, plan.name, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open a window for %q: %w", plan.name, err)
	}

	// The window goes the same way and for the same reason: a pane no row names runs
	// forever and shows up in no listing. It is separate from the check above only
	// because the pane does not exist until here.
	defer func() {
		if !launched {
			s.tmux.KillPane(pane)
		}
	}()

	agent := planned
	agent.Pane = pane

	if err := s.tmux.SetPaneOwner(pane, id); err != nil {
		return nil, fmt.Errorf("failed to stamp the pane for %q: %w", plan.name, err)
	}

	// The ceiling is claimed at the insert rather than at the check above, because a
	// transaction cannot be held across the setup action that may provision a worktree.
	// A create that loses the claim here unwinds through the defers above.
	if err := s.createUnderCeiling(ctx, agent); err != nil {
		return nil, err
	}

	// The row is read back rather than returned from what was written, since the store
	// owns the timestamps a caller renders.
	created, err := s.agents.GetInWorkflow(ctx, p.WorkflowID, plan.name)
	if err != nil {
		return nil, fmt.Errorf("failed to read back agent %q: %w", plan.name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("agent %q vanished after being recorded", plan.name)
	}

	if err := s.launcher.launch(plan.harness, launchParams{
		Pane:   pane,
		Dir:    dir,
		Flags:  plan.flags,
		Prompt: p.Prompt,
		// A snapshot taken at launch, so later edits to the workflow reach only agents
		// spawned after them.
		WorkflowContext: plan.workflowContext,
		SessionID:       sessionID,
		AgentID:         id,
	}); err != nil {
		// The pane and whatever setup built go with the unwind above. The row has to go
		// too, or the agent lists as live with nothing running behind it.
		if deleteErr := s.removeAgents(ctx, []string{id}); deleteErr != nil {
			return nil, fmt.Errorf("failed to launch the agent for %q (%w), and failed to clean up: %w", plan.name, err, deleteErr)
		}

		return nil, fmt.Errorf("failed to launch the agent for %q: %w", plan.name, err)
	}

	launched = true

	return created, nil
}

// DeleteAgents stops agents and removes them, keyed by id so it reaches the archived
// agents too. The workflow's cleanup action fires for each one between the pane and the
// row: after the pane so a script removing the directory is not racing a CLI still
// writing in it, and before the row so the script is told what the panel held.
//
// An id holding no agent refuses the whole batch, so a caller working from a stale
// listing does not destroy the agents it did name while learning about the one it did
// not.
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

// ArchiveAgents stops agents and hides them from the panel, keeping their records and
// transcripts so they can be restored later. DeleteAgents is the irreversible half.
//
// The agents need not share a workflow, and an id holding no agent refuses the batch the
// way it does a delete.
//
// The cleanup action does not fire here, since a restore relaunches in the directory
// the row records without running setup again, and tearing down what setup built would
// bring the agent back somewhere else without saying so.
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

// RestoreAgents takes agents out of the archive and returns them as stored. They land
// dormant, since starting anything is ResumeAgent's half. An unarchived agent holds a slot
// whether or not it is running, so this is refused at the ceiling a create is.
//
// The workflows the agents belong to come back with them, on one transaction, since an
// agent listed under a workflow nothing lists would get no row anywhere.
//
// It is all or nothing. A name a live agent has taken since is refused rather than worked
// around, since the panel would otherwise hand back an agent under a name nobody chose,
// and which of the two keeps the name is the user's to decide. A batch too big for the
// ceiling is refused whole for the same reason. Either refusal names what is in the way so
// it can be cleared and the key pressed again.
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

// requireEveryAgent reads the agents a batch names and refuses unless every id found one.
// A batch that acted on what it could find would leave the caller reading a success as
// having covered the whole list, and there is no undoing the half that landed.
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

// namedMissing is how many ids a refusal spells out before it starts counting. A batch may
// carry a hundred, and a message naming every one of them is drawn into a panel banner and
// read back by a model.
const namedMissing = 5

// describeMissing names the ids a batch could not find, counting the rest. What a caller
// does about it is the same whether five are missing or ninety, so the first few are enough
// to recognise the mistake by.
func describeMissing(missing []string) string {
	if len(missing) <= namedMissing {
		return strings.Join(missing, ", ")
	}

	return fmt.Sprintf("%s, and %d more", strings.Join(missing[:namedMissing], ", "), len(missing)-namedMissing)
}

// agentIDs is the ids of a set of agents, which is what the calls keyed on a list take.
func agentIDs(agents []agentstore.Agent) []string {
	return xslice.Map(agents, func(agent agentstore.Agent) string { return agent.ID })
}

// workflowIDs are the workflows a set of agents belongs to, each named once, so a batch
// spanning four folders brings back four rather than one per agent.
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

// ResumeAgent opens a window for an agent and relaunches its CLI on the same
// conversation, returning it as stored. An agent already running in its own pane is
// returned as it stands rather than started twice. An archived agent is refused, since
// its pane would sit behind a row nothing lists.
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

// isPaneOwnedBy compares a pane's stamp against the agent that claims it. An agent with
// no id owns nothing, so an unstamped pane cannot match by both sides being empty.
func isPaneOwnedBy(owner, agentID string) bool {
	return agentID != "" && owner == agentID
}

// killOwnedPanes tears down the panes of a set of agents, but only while each pane is
// still its agent's own, since a reissued id may now belong to another process. Liveness
// comes off one tmux call for the whole set rather than one per agent.
//
// It is a function rather than a method because archiving an agent by id and archiving
// the workflow around it stop the same CLIs from two different services.
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

// removeAgents deletes agents and the transcripts keyed to their ids in one transaction,
// so a removed agent leaves no conversation behind for the SQL tools to read.
func (s *AgentService) removeAgents(ctx context.Context, ids []string) error {
	return s.tx.InTx(ctx, func(db store.DBTX) error {
		if err := agentstore.New(db).Delete(ctx, ids); err != nil {
			return err
		}

		return transcript.New(db).Delete(ctx, ids)
	})
}

// createUnderCeiling records an agent on the same transaction that claims its slot, so the
// create that found room is the one that takes it and a caller that arrived with the panel
// already full is refused rather than landing beside the others.
func (s *AgentService) createUnderCeiling(ctx context.Context, agent agentstore.Agent) error {
	return s.tx.InTx(ctx, func(db store.DBTX) error {
		agentStore := agentstore.New(db)

		if err := claimRoomForAgents(ctx, agentStore, s.maxAgents, 1); err != nil {
			return err
		}

		if err := agentStore.Create(ctx, agent); err != nil {
			// uniqueAgentName checks before inserting, so two calls racing on the same
			// title both pass that check and one loses here.
			return fmt.Errorf("failed to record agent %q: %w", agent.Name, err)
		}

		return nil
	})
}

// requireUsableDir reports whether an agent can actually be started in dir. Existence is not
// enough: tmux silently falls back to its own default directory when it cannot enter
// the one given to -c, leaving the agent somewhere other than its stored cwd.
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

// claimRoomForAgents takes room for want more agents and holds it until the caller's
// transaction ends, so the count and the write it guards act as one. It has to be given a
// store bound to that same transaction, or the lock is released before the write lands.
func claimRoomForAgents(ctx context.Context, agents agentClaimer, maxAgents, want int) error {
	if maxAgents <= 0 || want <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForAgents(ctx, agents, maxAgents, want)
}

// requireRoomForAgents refuses unless want more agents fit under the ceiling. A workflow restore
// asks for its whole contents at once, since a folder that came back with some of its
// runs would read as whole. It only reads the count, so a caller about to write under it
// goes through claimRoomForAgents instead.
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

// launchPlan is a create read against the workflow it lands in.
type launchPlan struct {
	name string
	// harness is the CLI this agent will run, and harnessID is what the row records so
	// a later pass gets the same one.
	harness   harness.Harness
	harnessID string
	// dir is where the agent runs unless a setup action names somewhere else.
	dir             string
	flags           string
	workflowContext string
	// setupAction and cleanupAction are the workflow's, carried here so the launch and
	// the unwind after a failed one agree even if the workflow is edited in between.
	setupAction   string
	cleanupAction string
}

// planLaunch decides everything about a create that can be decided before anything is
// opened or written, so a create that cannot work fails here rather than part-way
// through with a window open and a row to take back out.
func (s *AgentService) planLaunch(ctx context.Context, p NewAgentParams) (*launchPlan, error) {
	// The workflow comes first, since it supplies the defaults everything below falls
	// back to.
	wf, err := s.workflows.Get(ctx, p.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", p.WorkflowID, err)
	}
	if wf == nil {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, p.WorkflowID)
	}

	// The harness is looked up before the directory is checked, since an agent naming a CLI
	// this binary cannot run is wrong about something no directory makes right.
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

	// What the caller asked for wins, then the workflow's default, then where the user
	// is looking. A scheduled launch supplies neither of the first two.
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

	// Flags follow the same rule. The panel prefills its form from the workflow, so only
	// a scheduled create arrives empty and takes them here.
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

// agent is the row this launch is about to write, and what the actions fired around the
// launch are told about. They are handed this rather than something read back, since
// most of the launch happens before the row exists.
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

// runSetup runs the workflow's setup action and returns the directory it named, blank
// when it named none. The answer is checked by the caller, where a refusal is still
// cleaned up after.
func (s *AgentService) runSetup(ctx context.Context, planned agentstore.Agent, name string) (string, error) {
	if s.lifecycle == nil || name == "" {
		return "", nil
	}

	return s.lifecycle.RunSetupAction(ctx, planned, name)
}

// runResumeSetup runs the workflow's setup action again for a dormant agent, and returns the
// directory it named. A refusal stops the resume, since the action is what makes the agent's
// workspace exist and coming back up without it would run the CLI over nothing.
func (s *AgentService) runResumeSetup(ctx context.Context, agent agentstore.Agent) (string, error) {
	name := s.getSetupAction(ctx, agent)
	if s.lifecycle == nil || name == "" {
		return "", nil
	}

	return s.lifecycle.RunResumeAction(ctx, agent, name)
}

// setupDir settles where an agent runs, given the directory the plan chose and the
// one its setup action named.
func (s *AgentService) setupDir(planned, named string) (string, error) {
	if named == "" {
		return planned, nil
	}

	return namedDir(named)
}

// namedDir is the directory a setup action named, blank when it named none. It is checked
// because nobody chose it: a script printing something that is not a directory has to be
// refused here rather than arrive later as a window opened somewhere unexpected.
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

// runCleanup fires the workflow's cleanup action for an agent that is going away. A
// failure is logged rather than returned, since an agent has to be deletable whatever
// state the actions directory is in, or a renamed script would leave a row nothing could
// remove.
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

// getCleanupAction is the action an agent's workflow names for a delete, blank when it
// cannot be read. A read that breaks is logged rather than returned, since the delete has
// nothing left to abort and a teardown skipped in silence reads as a workflow naming none.
func (s *AgentService) getCleanupAction(ctx context.Context, agent agentstore.Agent) string {
	wf := s.getWorkflowFor(ctx, agent)
	if wf == nil {
		return ""
	}

	return wf.CleanupAction
}

// getSetupAction is the action an agent's workflow names for a launch, blank when it cannot
// be read. A resume that cannot tell whether there is one comes back in the recorded
// directory, which is where it landed before this ran at all.
func (s *AgentService) getSetupAction(ctx context.Context, agent agentstore.Agent) string {
	wf := s.getWorkflowFor(ctx, agent)
	if wf == nil {
		return ""
	}

	return wf.SetupAction
}

// getWorkflowFor is the workflow an agent belongs to, nil when it cannot be read. It is read
// when an action fires rather than at launch, so editing a workflow reaches the agents
// already inside it. The archive is read through, since deleting an agent out of an archived
// workflow is where the teardown matters most.
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

// idempotencyKey renders a caller's key for storage. The column is unique, so an
// unclaimed create stores null rather than an empty string only one row could hold.
func idempotencyKey(key string) *string {
	if key == "" {
		return nil
	}

	return &key
}

// resume recreates a dormant agent's tmux window from its stored cwd and flags,
// updating the record. The row is already in the listing, so this touches nothing
// but the pane and what the panel reads to find it.
func (s *AgentService) resume(ctx context.Context, agent agentstore.Agent) error {
	// An agent recorded against a harness this binary no longer runs cannot be brought
	// back, and saying so beats a pane running the wrong CLI on its conversation.
	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return err
	}

	// Before the window, for the reason it runs before one at create: where the agent
	// comes back up is one of the things the action decides.
	named, err := s.runResumeSetup(ctx, agent)
	if err != nil {
		return err
	}

	dir, err := namedDir(named)
	if err != nil {
		return err
	}

	// An action that named nowhere leaves the recorded directory to be repaired, which is
	// also what an agent whose workflow has no setup action gets.
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
	// getResumeDir may have moved the agent, and the row is what the panel shows and the
	// next resume reads.
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

// uniqueAgentName derives an agent name from the title, falling back to a slug
// of the prompt, then "agent", and appends a suffix until it is free. Free means free
// inside the workflow, so the same name in two workflows takes no suffix.
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

// isNameTaken reports whether a live agent in the workflow already owns a name, since the
// insert would collide on the unique index.
func (s *AgentService) isNameTaken(ctx context.Context, workflowID, name string) bool {
	taken, err := s.agents.ActiveNameExists(ctx, workflowID, name)
	if err != nil {
		// A name that cannot be checked reads as free, and the insert then collides on the
		// index and fails with something vaguer than this. Logged so the cause is readable.
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to check whether an agent name is taken",
			append([]slog.Attr{slog.String(logging.WorkflowID, workflowID)}, logging.Err(err)...)...)
	}

	return taken
}
