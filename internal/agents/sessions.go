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

// tmuxClient is the part of the tmux client the manager drives. It is an
// interface so tests can substitute a recording fake for the real CLI.

// Rename retitles an agent. The name is a store-only display label and nothing in
// tmux keys on it, so no tmux object is renamed.
func (m *AgentManager) Rename(agent store.Agent, name string) {
	name = store.SanitizeName(strings.TrimSpace(name))
	if name == "" || name == agent.Name {
		return
	}

	_, _ = m.sessions.Rename(context.Background(), agent.ID, name)
}

// Delete soft-deletes an agent's record and kills its pane first. A caller
// showing that pane somewhere must stop showing it before calling, since killing
// it would take the containing layout down too.
func (m *AgentManager) Delete(agent store.Agent) {
	m.tmux.KillPane(agent.Pane)
	_ = m.sessions.SoftDelete(context.Background(), agent.Name)
}

// ErrSessionNotFound reports that no chat answers to the given name.
var ErrSessionNotFound = errors.New("session not found")

// ErrWorkflowNotFound reports that a chat named a workflow that does not exist.
var ErrWorkflowNotFound = errors.New("workflow not found")

// ErrBadDirectory reports a working directory the agent could not be started in.
var ErrBadDirectory = errors.New("unusable working directory")

// DeleteSession stops a chat and removes it. Killing the pane first is the part
// the store cannot do: deleting the row alone left the agent running, with
// nothing left that could name it or reach it.
func (m *AgentManager) DeleteSession(ctx context.Context, name string) error {
	agent, err := m.sessions.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to look up session %q: %w", name, err)
	}
	if agent == nil {
		return ErrSessionNotFound
	}

	m.tmux.KillPane(agent.Pane)

	deleted, err := m.sessions.Delete(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to delete session %q: %w", name, err)
	}
	if !deleted {
		return ErrSessionNotFound
	}

	return nil
}

// NewSessionParams configures a background agent to create.

// NewSessionParams configures a background agent to create.
type NewSessionParams struct {
	Client string
	Title  string
	Dir    string
	Flags  string
	Prompt string
	// WorkflowID scopes the new chat to a workflow, or is nil when it is created
	// from the root and belongs to no workflow.
	WorkflowID *string
}

// usableDir reports whether an agent can actually be started in dir. Existence
// is not enough: tmux falls back to its own default directory when it cannot
// enter the one given to -c, and it does that silently, so a directory that
// cannot be opened would produce a chat running somewhere other than the cwd
// stored against it.
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

// NewSession creates a background session, records it so it lists immediately,
// and launches claude with the default plus per-session flags and opening
// prompt. It does not switch the client to the new session. The stored record is
// returned so a caller that did not watch the list, such as an RPC client, still
// learns the chat's generated name and id.
//
// The directory is the given Dir, else the client's current directory.
func (m *AgentManager) NewSession(p NewSessionParams) (*store.Agent, error) {
	dir := expandHome(strings.TrimSpace(p.Dir))
	if dir == "" {
		dir = m.tmux.ClientPath(p.Client)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	// tmux silently falls back to its own default directory when -c names
	// something it cannot enter, so an unusable directory produced a chat running
	// somewhere other than the cwd stored against it. Checking first means the
	// launch fails instead of recording a location that is not true.
	if err := usableDir(dir); err != nil {
		return nil, err
	}

	// A chat created inside a workflow inherits the workflow's shared context,
	// injected as an appended system prompt at launch. It is a snapshot. Later
	// edits to the workflow context reach only sessions spawned after them. An
	// unknown workflow is refused rather than filed under an id that renders
	// nowhere.
	var workflowContext string
	if p.WorkflowID != nil {
		wf, err := m.sessions.GetWorkflow(context.Background(), *p.WorkflowID)
		if err != nil {
			return nil, fmt.Errorf("failed to look up workflow %q: %w", *p.WorkflowID, err)
		}
		if wf == nil {
			return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, *p.WorkflowID)
		}

		workflowContext = wf.Context
	}

	name := m.uniqueSessionName(p.Title, p.Prompt)

	pane, err := m.tmux.NewWindow(ManagerSession, name, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open a window for %q: %w", name, err)
	}
	flags := strings.TrimSpace(m.claudeFlags + " " + p.Flags)

	// Generate Claude's session id ourselves and launch with --session-id, so the
	// id is known up front and restoring the agent can resume that same
	// conversation with --resume.
	sessionID := uuid.NewString()
	if err := m.sessions.Upsert(context.Background(), store.Agent{
		Name:            name,
		Pane:            pane,
		Cwd:             dir,
		Flags:           flags,
		Prompt:          p.Prompt,
		State:           store.StateNew,
		ClaudeSessionID: sessionID,
		WorkflowID:      p.WorkflowID,
	}); err != nil {
		// uniqueSessionName checks before inserting, so two calls racing on the
		// same title both pass that check and one loses here. The window is
		// already open, and nothing else knows about it: without this kill it
		// would run forever, invisible to every listing.
		m.tmux.KillPane(pane)

		return nil, fmt.Errorf("failed to record session %q: %w", name, err)
	}

	if err := m.launchClaude(launchClaudeParams{
		Session:         pane,
		Flags:           flags,
		Prompt:          p.Prompt,
		WorkflowContext: workflowContext,
		SessionID:       sessionID,
	}); err != nil {
		// The row is already stored, and the pane is open with nothing useful in
		// it. Left alone it would list as a live chat forever, so both go.
		m.tmux.KillPane(pane)
		if _, deleteErr := m.sessions.Delete(context.Background(), name); deleteErr != nil {
			return nil, fmt.Errorf("failed to launch claude for %q (%w), and failed to clean up: %w", name, err, deleteErr)
		}

		return nil, fmt.Errorf("failed to launch claude for %q: %w", name, err)
	}

	// Read the row back rather than returning what was written: the store owns
	// the generated id and the created timestamp.
	created, err := m.sessions.Get(context.Background(), name)
	if err != nil {
		return nil, fmt.Errorf("failed to read back session %q: %w", name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("session %q vanished after being recorded", name)
	}

	return created, nil
}

// ListWorkflows returns every live workflow for the tree's folder headers.

// MoveChat reassigns a chat to a workflow, or unassigns it when workflowID is
// nil, keyed by the chat's stable id.
func (m *AgentManager) MoveChat(agent store.Agent, workflowID *string) {
	_ = m.sessions.MoveToWorkflow(context.Background(), agent.ID, workflowID)
}

// Restore recreates a dormant agent's tmux session from its stored cwd/flags
// and returns the new pane id, updating the stored record. A chat restored from
// search may be soft-deleted, so its deleted flag is cleared to return it to the
// normal listing.
func (m *AgentManager) Restore(agent store.Agent) (string, error) {
	dir := expandHome(agent.Cwd)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	// A deleted agent's original name may have been reused by a live agent while
	// it sat deleted. Undeleting it under that name would collide on the
	// unique-name index and fail, so give it a fresh unique name first, renaming
	// the stored row by id. A non-deleted dormant agent already owns its name, so
	// it is left as is.
	if agent.Deleted && m.nameTaken(agent.Name) {
		newName := m.uniqueSessionName(agent.Name, "")
		if _, err := m.sessions.Rename(context.Background(), agent.ID, newName); err != nil {
			return "", fmt.Errorf("failed to rename restored session %q: %w", agent.Name, err)
		}

		agent.Name = newName
	}

	pane, err := m.tmux.NewWindow(ManagerSession, agent.Name, dir)
	if err != nil {
		return "", fmt.Errorf("failed to open a window for %q: %w", agent.Name, err)
	}

	// Clear the soft-delete flag first, so the row re-enters the unique-name
	// index before the upsert below keys off it. Upserting a still-deleted row
	// would collide on the id primary key rather than resolve as a name conflict.
	if agent.Deleted {
		if err := m.sessions.Undelete(context.Background(), agent.ID); err != nil {
			return "", fmt.Errorf("failed to undelete session %q: %w", agent.Name, err)
		}
	}

	agent.Pane = pane
	agent.State = store.StateNew
	if err := m.sessions.Upsert(context.Background(), agent); err != nil {
		return "", fmt.Errorf("failed to restore session %q: %w", agent.Name, err)
	}

	if err := m.launchClaude(launchClaudeParams{
		Session:   pane,
		Flags:     agent.Flags,
		SessionID: agent.ClaudeSessionID,
		Resume:    true,
	}); err != nil {
		return "", fmt.Errorf("failed to relaunch claude for %q: %w", agent.Name, err)
	}

	return pane, nil
}

// OpenSearchHit opens the chat a search hit points at, keyed by its stable id,
// and returns the live pane and the chat's current name. It spans every chat,
// including soft-deleted ones. An already-live chat is returned as is. A dormant
// or deleted one is restored first, which relaunches Claude on its saved session
// and clears the deleted flag. An unknown id is an error.
func (m *AgentManager) OpenSearchHit(id string) (string, string, error) {
	agents, err := m.sessions.ListAll(context.Background())
	if err != nil {
		return "", "", fmt.Errorf("failed to list agents to open search hit: %w", err)
	}

	for _, agent := range agents {
		if agent.ID != id {
			continue
		}
		if m.tmux.PaneAlive(agent.Pane) {
			return agent.Pane, agent.Name, nil
		}

		pane, err := m.Restore(agent)
		if err != nil {
			return "", "", err
		}

		// Restore may rename a deleted chat whose name was reused while it sat
		// deleted, so re-resolve the name by id rather than trusting the stale one.
		return pane, m.nameByID(context.Background(), id, agent.Name), nil
	}

	return "", "", fmt.Errorf("no chat with id %q", id)
}

// nameByID returns the current name of the chat with the given id, falling back
// to fallback when the lookup fails, so a caller can position on a chat whose
// name may have just changed.
func (m *AgentManager) nameByID(ctx context.Context, id, fallback string) string {
	agents, err := m.sessions.ListAll(ctx)
	if err != nil {
		return fallback
	}

	for _, agent := range agents {
		if agent.ID == id {
			return agent.Name
		}
	}

	return fallback
}

// uniqueSessionName derives a session name from the title, falling back to a
// slug of the prompt, then "agent", and appends a suffix until it is free.
func (m *AgentManager) uniqueSessionName(title, prompt string) string {
	base := store.SanitizeName(strings.TrimSpace(title))
	if base == "" {
		base = xstr.Slug(xstr.FirstLine(prompt), 24)
	}
	if base == "" {
		base = "agent"
	}

	name := base
	for i := 2; m.nameTaken(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}

	return name
}

// nameTaken reports whether a name is already owned by a live agent record. The
// store's unique-name index is the single authority. Reusing a name would make
// the upsert clobber that agent's row instead of creating a fresh one.
func (m *AgentManager) nameTaken(name string) bool {
	taken, _ := m.sessions.ActiveNameExists(context.Background(), name)
	return taken
}

// mcpBriefing tells a spawned agent that the panel's own database is queryable,
// so a question about the other chats is something it can look up rather than
// guess at. The closing line matters as much as the rest: an agent whose client
// has not registered the server must say so instead of inventing an answer.
