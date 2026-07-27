package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-agents/internal/store"
	"claude-agents/lib/xstr"

	agentstore "claude-agents/internal/store/agent"
	"claude-agents/internal/store/transcript"
	"claude-agents/internal/store/workflow"

	"github.com/google/uuid"
)

// expandHome expands a leading ~ or ~/ to the user's home. tmux's new-session -c
// does no shell tilde expansion, so a stored ~ path would fail to chdir.
// Expanding here keeps the cwd absolute.
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

// agentStore is the part of the agent store this service reads and mutates.
type agentStore interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
	Get(ctx context.Context, name string) (*agentstore.Agent, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*agentstore.Agent, error)
	Find(ctx context.Context, name string) (*agentstore.Target, error)
	Upsert(ctx context.Context, agent agentstore.Agent) error
	Rename(ctx context.Context, id, name string) (bool, error)
	Archive(ctx context.Context, name string) error
	Restore(ctx context.Context, id string) error
	ActiveNameExists(ctx context.Context, name string) (bool, error)
}

// workflowReader is all this service needs from the workflow domain. An agent
// spawned inside a workflow snapshots that workflow's shared context at launch,
// so this reads and never writes.
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

// agentTmux is the part of tmux an agent's lifecycle needs. A window to live in,
// a way to end it, and a way to tell whether it is still there.
type agentTmux interface {
	NewWindow(session, name, dir string) (string, error)
	KillPane(pane string)
	PaneAlive(pane string) bool
	ClientPath(client string) string
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
	// claudeFlags are the configured defaults every agent launches with, recorded
	// on the row alongside whatever the caller added.
	claudeFlags string
}

// AgentParams carries the agent service's dependencies.
type AgentParams struct {
	Tmux        agentTmux
	Agents      agentStore
	Workflows   workflowReader
	Tx          txRunner
	Launcher    *Launcher
	ClaudeFlags string
}

// NewAgentService builds the service from its dependencies.
func NewAgentService(p AgentParams) *AgentService {
	return &AgentService{
		tmux:        p.Tmux,
		agents:      p.Agents,
		workflows:   p.Workflows,
		tx:          p.Tx,
		launcher:    p.Launcher,
		claudeFlags: p.ClaudeFlags,
	}
}

// PaneAlive reports whether an agent's tmux pane still exists.
func (s *AgentService) PaneAlive(pane string) bool {
	return s.tmux.PaneAlive(pane)
}

// ErrAgentNotFound reports that no agent answers to the given name or id.
var ErrAgentNotFound = errors.New("agent not found")

// ErrWorkflowNotFound reports that an agent named a workflow that does not exist.
var ErrWorkflowNotFound = errors.New("workflow not found")

// ErrBadDirectory reports a working directory the agent could not be started in.
var ErrBadDirectory = errors.New("unusable working directory")

// DeleteAgent stops an agent and removes it. The pane is killed first, which the
// store cannot do. Dropping the row alone leaves the agent running with nothing
// left that can name it or reach it.
func (s *AgentService) DeleteAgent(ctx context.Context, name string) error {
	agent, err := s.agents.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to look up agent %q: %w", name, err)
	}
	if agent == nil {
		return ErrAgentNotFound
	}

	s.tmux.KillPane(agent.Pane)

	return s.removeAgent(ctx, name)
}

// removeAgent deletes an agent, the config it owns, and the transcript keyed to
// its id, in one transaction. The transcript is the transcript package's table,
// so the two stores are rebound onto the same transaction and commit together.
// An agent removed without its text would leave the conversation readable
// through the SQL tools.
func (s *AgentService) removeAgent(ctx context.Context, name string) error {
	target, err := s.agents.Find(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to find agent %q to delete: %w", name, err)
	}
	if target == nil {
		return ErrAgentNotFound
	}

	return s.tx.InTx(ctx, func(db store.DBTX) error {
		if err := agentstore.New(db).Delete(ctx, *target); err != nil {
			return err
		}

		return transcript.New(db).Delete(ctx, target.ID)
	})
}

// ArchiveAgent stops an agent and hides it from the panel, keyed by name. The
// record and its transcript stay, so search still reaches it and it can be
// restored later. This is what the panel's delete key does. DeleteAgent is the
// irreversible half.
func (s *AgentService) ArchiveAgent(ctx context.Context, name string) error {
	agent, err := s.agents.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to look up agent %q: %w", name, err)
	}
	if agent == nil {
		return ErrAgentNotFound
	}

	s.tmux.KillPane(agent.Pane)

	if err := s.agents.Archive(ctx, name); err != nil {
		return fmt.Errorf("failed to archive agent %q: %w", name, err)
	}

	return nil
}

// RestoreAgent brings an agent back by id, relaunching claude on the same
// conversation, and returns it as stored. It spans archived agents, since
// restoring one is the point. An id that matches nothing is ErrAgentNotFound.
func (s *AgentService) RestoreAgent(ctx context.Context, id string) (*agentstore.Agent, error) {
	agents, err := s.agents.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents to restore %q: %w", id, err)
	}

	for _, agent := range agents {
		if agent.ID != id {
			continue
		}

		// An agent whose pane is still alive is already running. Returning it as it
		// stands keeps the call idempotent rather than spawning a second window
		// for the same conversation.
		if s.tmux.PaneAlive(agent.Pane) {
			return &agent, nil
		}

		if err := s.restore(ctx, agent); err != nil {
			return nil, err
		}

		// Restore may rename an agent whose name was taken while it sat archived, so
		// the row is read back rather than returned from the stale copy.
		restored, err := s.byID(ctx, id)
		if err != nil {
			return nil, err
		}

		return restored, nil
	}

	return nil, ErrAgentNotFound
}

// byID returns the agent with the given id, spanning archived ones.
func (s *AgentService) byID(ctx context.Context, id string) (*agentstore.Agent, error) {
	agents, err := s.agents.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	for _, agent := range agents {
		if agent.ID == id {
			return &agent, nil
		}
	}

	return nil, ErrAgentNotFound
}

// NewAgentParams configures a background agent to create.
type NewAgentParams struct {
	Client string
	Title  string
	Dir    string
	Flags  string
	Prompt string
	// WorkflowID is the workflow the agent belongs to, and is required. It supplies
	// the directory and flags when Dir and Flags are left empty, and the shared
	// context appended to the agent's system prompt.
	WorkflowID string
	// ScheduleID records the schedule that launched this agent, nil for one a
	// person started. Provenance only.
	ScheduleID *string
	// IdempotencyKey makes the create repeatable. A second call carrying a key an
	// agent already holds returns that agent untouched rather than spawning a
	// second Claude process, which is what makes a retried background launch safe.
	// Empty makes no claim, which is every create from the panel.
	IdempotencyKey string
}

// usableDir reports whether an agent can actually be started in dir. Existence
// is not enough. tmux silently falls back to its own default directory when it
// cannot enter the one given to -c, so a directory that cannot be opened would
// leave an agent running somewhere other than the cwd stored against it.
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
// launches claude with the default plus per-agent flags and the opening prompt.
// It does not switch the client to the new agent. The stored record is returned
// so a caller that did not watch the list, such as an RPC client, still learns
// the generated name and id.
//
// The directory is the given Dir, else the client's current directory.
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

	// The workflow comes first, since it supplies the defaults everything below
	// falls back to. An unknown one is refused rather than filed under an id that
	// renders nowhere.
	wf, err := s.workflows.Get(ctx, p.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", p.WorkflowID, err)
	}
	if wf == nil {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, p.WorkflowID)
	}

	// What the caller asked for wins, then the workflow's default, then where the
	// user is looking. A scheduled launch supplies neither of the first two, which
	// is the point of the workflow holding them.
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

	// Checking the directory first means the launch fails rather than recording a
	// location that is not true.
	if err := usableDir(dir); err != nil {
		return nil, err
	}

	// Flags follow the same rule. The panel prefills its form from the workflow, so
	// an interactive create arrives with them already set and this changes nothing.
	// A scheduled one arrives empty and takes them here.
	agentFlags := strings.TrimSpace(p.Flags)
	if agentFlags == "" {
		agentFlags = strings.TrimSpace(wf.Flags)
	}

	name := s.uniqueAgentName(ctx, p.Title, p.Prompt)

	pane, err := s.tmux.NewWindow(ManagerSession, name, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open a window for %q: %w", name, err)
	}
	flags := strings.TrimSpace(s.claudeFlags + " " + agentFlags)

	// Generate Claude's session id here and launch with --session-id, so the id is
	// known up front and restoring the agent can resume that same conversation
	// with --resume.
	claudeSessionID := uuid.NewString()
	if err := s.agents.Upsert(ctx, agentstore.Agent{
		Name:            name,
		Pane:            pane,
		Cwd:             dir,
		Flags:           flags,
		Prompt:          p.Prompt,
		State:           agentstore.StateNew,
		ClaudeSessionID: claudeSessionID,
		WorkflowID:      p.WorkflowID,
		ScheduleID:      p.ScheduleID,
		IdempotencyKey:  idempotencyKey(p.IdempotencyKey),
	}); err != nil {
		// uniqueAgentName checks before inserting, so two calls racing on the same
		// title both pass that check and one loses here. The window is already open
		// and nothing else knows about it, so without this kill it would run
		// forever, invisible to every listing.
		s.tmux.KillPane(pane)

		return nil, fmt.Errorf("failed to record agent %q: %w", name, err)
	}

	if err := s.launcher.launch(launchClaudeParams{
		Pane:   pane,
		Flags:  flags,
		Prompt: p.Prompt,
		// The workflow's shared context is appended to the system prompt at launch.
		// It is a snapshot, so later edits to the workflow reach only agents spawned
		// after them.
		WorkflowContext: wf.Context,
		ClaudeSessionID: claudeSessionID,
	}); err != nil {
		// The row is stored and the pane is open with nothing useful in it. Left
		// alone it would list as a live agent forever, so both go.
		s.tmux.KillPane(pane)
		if deleteErr := s.removeAgent(ctx, name); deleteErr != nil {
			return nil, fmt.Errorf("failed to launch claude for %q (%w), and failed to clean up: %w", name, err, deleteErr)
		}

		return nil, fmt.Errorf("failed to launch claude for %q: %w", name, err)
	}

	// Read the row back rather than returning what was written. The store owns the
	// generated id and the created timestamp.
	created, err := s.agents.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to read back agent %q: %w", name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("agent %q vanished after being recorded", name)
	}

	return created, nil
}

// idempotencyKey renders a caller's key for storage. The column is unique across
// every row, so an unclaimed create stores null rather than an empty string,
// which only the first of them could hold.
func idempotencyKey(key string) *string {
	if key == "" {
		return nil
	}

	return &key
}

// restore recreates a dormant agent's tmux window from its stored cwd and flags,
// updating the record. An archived agent has its hidden flag cleared, which is
// what returns it to the normal listing.
func (s *AgentService) restore(ctx context.Context, agent agentstore.Agent) error {
	dir := expandHome(agent.Cwd)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	// A deleted agent's original name may have been reused by a live agent while
	// it sat deleted. Undeleting it under that name would collide on the
	// unique-name index and fail, so give it a fresh unique name first, renaming
	// the stored row by id. A non-deleted dormant agent already owns its name, so
	// it is left as is.
	if agent.Deleted && s.nameTaken(ctx, agent.Name) {
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

	// Clear the soft-delete flag first, so the row re-enters the unique-name
	// index before the upsert below keys off it. Upserting a still-deleted row
	// would collide on the id primary key rather than resolve as a name conflict.
	if agent.Deleted {
		if err := s.agents.Restore(ctx, agent.ID); err != nil {
			return fmt.Errorf("failed to unarchive agent %q: %w", agent.Name, err)
		}
	}

	agent.Pane = pane
	agent.State = agentstore.StateNew
	if err := s.agents.Upsert(ctx, agent); err != nil {
		return fmt.Errorf("failed to restore agent %q: %w", agent.Name, err)
	}

	if err := s.launcher.launch(launchClaudeParams{
		Pane:            pane,
		Flags:           agent.Flags,
		ClaudeSessionID: agent.ClaudeSessionID,
		Resume:          true,
	}); err != nil {
		return fmt.Errorf("failed to relaunch claude for %q: %w", agent.Name, err)
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

// nameTaken reports whether a name is already owned by a live agent record.
// Reusing a name would make the upsert clobber that agent's row instead of
// creating a fresh one.
func (s *AgentService) nameTaken(ctx context.Context, name string) bool {
	taken, _ := s.agents.ActiveNameExists(ctx, name)
	return taken
}
