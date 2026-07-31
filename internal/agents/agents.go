package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"weave/internal/store"
	"weave/lib/xstr"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	"weave/internal/store/transcript"
	"weave/internal/store/workflow"
)

// expandHome expands a leading ~ or ~/ to the user's home. tmux's new-session -c
// does no shell tilde expansion, so a stored ~ path would fail to chdir.
func expandHome(dir string) string {
	if dir != "~" && !strings.HasPrefix(dir, "~/") {
		return dir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return dir
	}
	if dir == "~" {
		return home
	}

	return filepath.Join(home, dir[len("~/"):])
}

type agentStore interface {
	Get(ctx context.Context, name string) (*agentstore.Agent, error)
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*agentstore.Agent, error)
	Exists(ctx context.Context, id string) (bool, error)
	Create(ctx context.Context, agent agentstore.Agent) error
	Update(ctx context.Context, agent agentstore.Agent) error
	Rename(ctx context.Context, id, name string) (bool, error)
	Archive(ctx context.Context, name string) error
	Restore(ctx context.Context, id string) error
	ActiveNameExists(ctx context.Context, name string) (bool, error)
}

// workflowReader is all this service needs from the workflow domain. An agent
// snapshots its workflow's shared context at launch, so this reads and never
// writes.
type workflowReader interface {
	Get(ctx context.Context, id string) (*workflow.Workflow, error)
}

// txRunner opens the transaction a write spanning two domains needs. Removing an
// agent also removes its transcript, which lives in another package, so the two
// stores are rebound onto one transaction instead of either reaching into the
// other's tables.
type txRunner interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

type agentTmux interface {
	NewWindow(session, name, dir string) (string, error)
	KillPane(pane string)
	SetPaneOwner(pane, agentID string) error
	PaneOwner(pane string) string
	ClientPath(client string) string
}

// harnessRegistry resolves the CLI an agent names. It hands back the whole Harness
// because a lookup cannot return a different interface per caller.
type harnessRegistry interface {
	Resolve(id string) (harness.Harness, error)
}

// AgentService owns an agent's lifecycle. It creates one, brings a dormant one
// back, renames, archives, and removes it, coordinating the store with tmux and
// delegating the command line to the launcher.
type AgentService struct {
	tmux      agentTmux
	agents    agentStore
	workflows workflowReader
	tx        txRunner
	launcher  *Launcher
	harnesses harnessRegistry
	// defaultHarness is the CLI an agent runs when neither it nor its workflow names
	// one, and defaultFlags are the configured flags each harness's agents are
	// launched with, ahead of their own, keyed by harness id.
	defaultHarness string
	defaultFlags   map[string]string
	// restoreRoot is only read when a restore has to repair a working directory that
	// is gone. It is a field rather than a constant so a test can point it at a temp
	// directory instead of the user's home.
	restoreRoot string
}

// AgentParams carries the agent service's dependencies.
type AgentParams struct {
	Tmux           agentTmux
	Agents         agentStore
	Workflows      workflowReader
	Tx             txRunner
	Launcher       *Launcher
	Harnesses      harnessRegistry
	DefaultHarness string
	DefaultFlags   map[string]string
	// RestoreRoot is where an agent runs when its recorded directory cannot be
	// recreated, defaulting to the real location when left empty.
	RestoreRoot string
}

// NewAgentService builds the service from its dependencies.
func NewAgentService(p AgentParams) *AgentService {
	if p.RestoreRoot == "" {
		p.RestoreRoot = DefaultRestoreRoot()
	}

	return &AgentService{
		tmux:           p.Tmux,
		agents:         p.Agents,
		workflows:      p.Workflows,
		tx:             p.Tx,
		launcher:       p.Launcher,
		harnesses:      p.Harnesses,
		defaultHarness: p.DefaultHarness,
		defaultFlags:   p.DefaultFlags,
		restoreRoot:    p.RestoreRoot,
	}
}

// AgentRunning reports whether an agent's CLI is still up, which is true only
// of a pane that exists and is stamped with that agent's id. The stored pane id is
// never trusted on its own: tmux reissues pane ids from %0 after a server restart,
// so a dormant agent's recorded pane routinely names a pane that now belongs to
// somebody else.
func (s *AgentService) AgentRunning(agent agentstore.Agent) bool {
	return paneOwnedBy(s.tmux.PaneOwner(agent.Pane), agent.ID)
}

// paneOwnedBy compares a pane's stamp against the agent that claims it. An agent
// with no id owns nothing, so an unstamped pane cannot answer to it by both sides
// being empty.
func paneOwnedBy(owner, agentID string) bool {
	return agentID != "" && owner == agentID
}

// killOwnedPane tears down an agent's pane, but only while that pane is still the
// agent's own. Killing by a stored id alone would end whatever process the tmux
// server has since given that id to.
func (s *AgentService) killOwnedPane(agent agentstore.Agent) {
	if s.AgentRunning(agent) {
		s.tmux.KillPane(agent.Pane)
	}
}

// ErrAgentNotFound reports that no agent answers to the given name or id.
var ErrAgentNotFound = errors.New("agent not found")

// ErrWorkflowNotFound reports that an agent named a workflow that does not exist.
var ErrWorkflowNotFound = errors.New("workflow not found")

// ErrBadDirectory reports a working directory the agent could not be started in.
var ErrBadDirectory = errors.New("unusable working directory")

// DeleteAgent stops an agent and removes it, keyed by id so it reaches the
// archived agents too. Dropping the row alone would leave the agent running with
// nothing left that can name it or reach it.
func (s *AgentService) DeleteAgent(ctx context.Context, id string) error {
	agent, err := s.byID(ctx, id)
	if err != nil {
		return err
	}

	s.killOwnedPane(*agent)

	return s.removeAgent(ctx, id)
}

// removeAgent deletes an agent and the transcript keyed to its id in one
// transaction, since an agent removed without its text would leave the conversation
// readable through the SQL tools.
func (s *AgentService) removeAgent(ctx context.Context, id string) error {
	exists, err := s.agents.Exists(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to look up agent %q to delete: %w", id, err)
	}
	if !exists {
		return ErrAgentNotFound
	}

	return s.tx.InTx(ctx, func(db store.DBTX) error {
		if err := agentstore.New(db).Delete(ctx, id); err != nil {
			return err
		}

		return transcript.New(db).Delete(ctx, id)
	})
}

// ArchiveAgent stops an agent and hides it from the panel, keyed by name. The
// record and its transcript stay, so search still reaches it and it can be restored
// later. DeleteAgent is the irreversible half.
func (s *AgentService) ArchiveAgent(ctx context.Context, name string) error {
	agent, err := s.agents.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to look up agent %q: %w", name, err)
	}
	if agent == nil {
		return ErrAgentNotFound
	}

	s.killOwnedPane(*agent)

	if err := s.agents.Archive(ctx, name); err != nil {
		return fmt.Errorf("failed to archive agent %q: %w", name, err)
	}

	return nil
}

// RestoreAgent brings an agent back by id, relaunching its CLI on the same
// conversation, and returns it as stored. It spans archived agents, since
// restoring one is the point. An id that matches nothing is ErrAgentNotFound.
func (s *AgentService) RestoreAgent(ctx context.Context, id string) (*agentstore.Agent, error) {
	agent, err := s.byID(ctx, id)
	if err != nil {
		return nil, err
	}

	// An agent still running in its own pane needs no restore, so the call is
	// idempotent rather than spawning a second window for the same conversation.
	if s.AgentRunning(*agent) {
		return agent, nil
	}

	if err := s.restore(ctx, *agent); err != nil {
		return nil, err
	}

	// Restore may rename an agent whose name was taken while it sat archived, so the
	// row is read back rather than returned from the copy taken before the write.
	return s.byID(ctx, id)
}

func (s *AgentService) byID(ctx context.Context, id string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, ErrAgentNotFound
	}

	return agent, nil
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
	// IdempotencyKey makes the create repeatable. A second call carrying a key an
	// agent already holds returns that agent untouched rather than spawning a second
	// Claude process. Empty makes no claim, which is every create from the panel.
	IdempotencyKey string
}

// usableDir reports whether an agent can actually be started in dir. Existence is
// not enough: tmux silently falls back to its own default directory when it cannot
// enter the one given to -c, which would leave an agent running somewhere other than
// the cwd stored against it.
func usableDir(dir string) error {
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

// NewAgent creates a background agent, records it so it lists immediately, and
// launches its CLI with the default plus per-agent flags and the opening prompt. It
// does not switch the client to the new agent. The stored record is returned so a
// caller that did not watch the list still learns the generated name and id.
func (s *AgentService) NewAgent(ctx context.Context, p NewAgentParams) (*agentstore.Agent, error) {
	// The key is checked before anything is opened or written, so a retry costs a
	// lookup rather than a second tmux window that has to be cleaned up after.
	if p.IdempotencyKey != "" {
		existing, err := s.agents.GetByIdempotencyKey(ctx, p.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}

	plan, err := s.planLaunch(ctx, p)
	if err != nil {
		return nil, err
	}

	// The id is minted here rather than by the insert below, because the pane has to
	// be stamped with it before the row exists. The reconciler reads a row against an
	// unstamped pane as belonging to nobody and clears the pane out from under an agent
	// about to launch in it.
	id, err := store.NewID()
	if err != nil {
		return nil, err
	}

	pane, err := s.tmux.NewWindow(ManagerSession, plan.name, plan.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open a window for %q: %w", plan.name, err)
	}

	// Every failure from here on has to take the window down again, since a pane no
	// row names runs forever and shows up in no listing. One deferred check covers
	// them all, so a failure path added later cannot forget the kill.
	launched := false
	defer func() {
		if !launched {
			s.tmux.KillPane(pane)
		}
	}()

	if err := s.tmux.SetPaneOwner(pane, id); err != nil {
		return nil, fmt.Errorf("failed to stamp the pane for %q: %w", plan.name, err)
	}

	// The conversation is named before it exists, so restoring the agent can ask the
	// harness to reopen that same one. A harness that will not be told a name returns
	// none, leaving the id to be discovered after the launch.
	sessionID := plan.harness.MintSessionID()
	if err := s.agents.Create(ctx, agentstore.Agent{
		ID:             id,
		Name:           plan.name,
		Pane:           pane,
		Cwd:            plan.dir,
		Flags:          plan.flags,
		Prompt:         p.Prompt,
		State:          agentstore.StateNew,
		Harness:        plan.harnessID,
		SessionID:      sessionID,
		WorkflowID:     p.WorkflowID,
		ScheduleID:     p.ScheduleID,
		IdempotencyKey: idempotencyKey(p.IdempotencyKey),
		Hidden:         p.Hidden,
	}); err != nil {
		// uniqueAgentName checks before inserting, so two calls racing on the same
		// title both pass that check and one loses here.
		return nil, fmt.Errorf("failed to record agent %q: %w", plan.name, err)
	}

	// The row is read back rather than returned from what was written, since the store
	// owns the id and timestamps a caller renders.
	created, err := s.agents.Get(ctx, plan.name)
	if err != nil {
		return nil, fmt.Errorf("failed to read back agent %q: %w", plan.name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("agent %q vanished after being recorded", plan.name)
	}

	if err := s.launcher.launch(plan.harness, launchParams{
		Pane:   pane,
		Dir:    plan.dir,
		Flags:  plan.flags,
		Prompt: p.Prompt,
		// The workflow's context is a snapshot taken at launch, so later edits to the
		// workflow reach only agents spawned after them.
		WorkflowContext: plan.workflowContext,
		SessionID:       sessionID,
		AgentID:         id,
	}); err != nil {
		// The pane goes with the unwind above. The row has to go too, or the agent
		// lists as live forever with nothing running behind it.
		if deleteErr := s.removeAgent(ctx, id); deleteErr != nil {
			return nil, fmt.Errorf("failed to launch the agent for %q (%w), and failed to clean up: %w", plan.name, err, deleteErr)
		}

		return nil, fmt.Errorf("failed to launch the agent for %q: %w", plan.name, err)
	}

	launched = true

	return created, nil
}

// launchPlan is a create resolved against the workflow it lands in.
type launchPlan struct {
	name string
	// harness is the CLI this agent will run, and harnessID is what the row records so
	// a later pass resolves the same one.
	harness         harness.Harness
	harnessID       string
	dir             string
	flags           string
	workflowContext string
}

// planLaunch decides everything about a create that can be decided before anything is
// opened or written. It touches nothing outside the store it reads, so a create that
// cannot work fails here rather than part-way through, with a window open and a row to
// take back out.
func (s *AgentService) planLaunch(ctx context.Context, p NewAgentParams) (*launchPlan, error) {
	// The workflow comes first, since it supplies the defaults everything below falls
	// back to. An unknown one is refused rather than filed under an id that renders
	// nowhere.
	wf, err := s.workflows.Get(ctx, p.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", p.WorkflowID, err)
	}
	if wf == nil {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, p.WorkflowID)
	}

	// The harness resolves before the directory is checked, since an agent naming a
	// CLI this binary cannot run is wrong about something no directory makes right.
	harnessID := strings.TrimSpace(p.Harness)
	if harnessID == "" {
		harnessID = strings.TrimSpace(wf.Harness)
	}
	if harnessID == "" {
		harnessID = s.defaultHarness
	}

	resolved, err := s.harnesses.Resolve(harnessID)
	if err != nil {
		return nil, err
	}

	// What the caller asked for wins, then the workflow's default, then where the user
	// is looking. A scheduled launch supplies neither of the first two, which is the
	// point of the workflow holding them.
	dir := expandHome(strings.TrimSpace(p.Dir))
	if dir == "" {
		dir = expandHome(strings.TrimSpace(wf.Cwd))
	}
	if dir == "" {
		dir = s.tmux.ClientPath(p.Client)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	if err := usableDir(dir); err != nil {
		return nil, err
	}

	// Flags follow the same rule. The panel prefills its form from the workflow, so an
	// interactive create arrives with them already set and this changes nothing. A
	// scheduled one arrives empty and takes them here.
	agentFlags := strings.TrimSpace(p.Flags)
	if agentFlags == "" {
		agentFlags = strings.TrimSpace(wf.Flags)
	}

	return &launchPlan{
		name:            s.uniqueAgentName(ctx, p.Title, p.Prompt),
		harness:         resolved,
		harnessID:       harnessID,
		dir:             dir,
		flags:           strings.TrimSpace(s.defaultFlags[harnessID] + " " + agentFlags),
		workflowContext: wf.Context,
	}, nil
}

// idempotencyKey renders a caller's key for storage. The column is unique, so an
// unclaimed create stores null rather than an empty string only the first of them
// could hold.
func idempotencyKey(key string) *string {
	if key == "" {
		return nil
	}

	return &key
}

// restore recreates a dormant agent's tmux window from its stored cwd and flags,
// updating the record. An archived agent has its deletion time cleared, which is
// what returns it to the normal listing.
func (s *AgentService) restore(ctx context.Context, agent agentstore.Agent) error {
	// An agent recorded against a harness this binary no longer runs cannot be brought
	// back, and saying so beats a pane running the wrong CLI against its conversation.
	resolved, err := s.harnesses.Resolve(agent.Harness)
	if err != nil {
		return err
	}

	dir := s.resumeDir(agent, resolved)

	// A deleted agent's name may have been reused while it sat deleted, and undeleting
	// it under that name would collide on the unique-name index, so it takes a fresh
	// one first. A non-deleted dormant agent already owns its name.
	if agent.DeletedAt != nil && s.nameTaken(ctx, agent.Name) {
		newName := s.uniqueAgentName(ctx, agent.Name, "")
		if _, err := s.agents.Rename(ctx, agent.ID, newName); err != nil {
			return fmt.Errorf("failed to rename restored agent %q: %w", agent.Name, err)
		}

		agent.Name = newName
	}

	pane, err := s.tmux.NewWindow(ManagerSession, agent.Name, dir)
	if err != nil {
		return fmt.Errorf("failed to open a window for %q: %w", agent.Name, err)
	}
	if err := s.tmux.SetPaneOwner(pane, agent.ID); err != nil {
		s.tmux.KillPane(pane)

		return fmt.Errorf("failed to stamp the pane for %q: %w", agent.Name, err)
	}

	// Update below writes neither the deletion time nor the name, so clearing it is
	// its own call.
	if agent.DeletedAt != nil {
		if err := s.agents.Restore(ctx, agent.ID); err != nil {
			return fmt.Errorf("failed to unarchive agent %q: %w", agent.Name, err)
		}
	}

	agent.Pane = pane
	agent.State = agentstore.StateNew
	// resumeDir may have moved the agent somewhere else, and the row is what the panel
	// shows and the next restore reads.
	agent.Cwd = dir
	if err := s.agents.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to restore agent %q: %w", agent.Name, err)
	}

	if err := s.launcher.launch(resolved, launchParams{
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
// of the prompt, then "agent", and appends a suffix until it is free.
func (s *AgentService) uniqueAgentName(ctx context.Context, title, prompt string) string {
	base := agentstore.SanitizeName(strings.TrimSpace(title))
	if base == "" {
		base = xstr.Slug(xstr.FirstLine(prompt), 24)
	}
	if base == "" {
		base = "agent"
	}

	name := base
	for i := 2; s.nameTaken(ctx, name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}

	return name
}

// nameTaken reports whether a name is already owned by a live agent record. Reusing
// one would make the upsert clobber that agent's row instead of creating a fresh
// one.
func (s *AgentService) nameTaken(ctx context.Context, name string) bool {
	taken, _ := s.agents.ActiveNameExists(ctx, name)
	return taken
}
