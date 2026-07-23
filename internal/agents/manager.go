// Package agents orchestrates the store and tmux to implement both the panel
// actions and the CLI subcommands (notify, status, hook, chat).
package agents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"claude-agents/internal/store"
	"claude-agents/lib/xstr"

	"github.com/google/uuid"
)

// previewScrollTicks is how many wheel notches one preview scroll sends, so a
// keypress moves the agent's view a noticeable amount.
const previewScrollTicks = 4

// tmuxClient is the slice of the tmux client the manager drives. Accepting it as
// an interface lets tests substitute a recording fake for the real CLI.
type tmuxClient interface {
	Socket() string
	SetGlobal(name, value string)
	PaneAlive(pane string) bool
	HasSession(name string) bool
	SessionAttached(session string) bool
	ResizeWindow(target string, width, height int)
	CapturePane(pane string) string
	ScrollPane(pane string, width, height int, up bool, ticks int)
	RenameSession(oldName, newName string)
	ClientSession(client string) string
	SwitchToLast(client string)
	KillSession(name string)
	ClientPath(client string) string
	NewDetachedSession(name, dir string)
	PaneID(target string) (string, error)
	SendKeys(target string, keys ...string)
	CurrentSession() (string, error)
	SetWindowSizeLatest(target string)
}

// agentStore is the slice of the store the manager reads and mutates. Accepting
// it as an interface keeps the manager's business logic testable against a real
// store without pulling in a concrete type.
type agentStore interface {
	List(ctx context.Context) ([]store.Agent, error)
	ListAll(ctx context.Context) ([]store.Agent, error)
	Upsert(ctx context.Context, agent store.Agent) error
	UpdateState(ctx context.Context, name, pane, state, claudeSessionID string) error
	SetState(ctx context.Context, name, state string) error
	SetStateByID(ctx context.Context, id, state string) error
	Rename(ctx context.Context, id, name string) error
	SoftDelete(ctx context.Context, name string) error
	Undelete(ctx context.Context, id string) error
	ActiveNameExists(ctx context.Context, name string) (bool, error)
}

// AgentManager lists, launches, and acts on Claude agents by coordinating a
// tmux client and the SQLite-backed store.
type AgentManager struct {
	tmux        tmuxClient
	sessions    agentStore
	selfPath    string
	claudeFlags string
}

// Params carries the AgentManager's dependencies.
type Params struct {
	Tmux        tmuxClient
	Store       agentStore
	SelfPath    string
	ClaudeFlags string
}

// NewAgentManager constructs an AgentManager from its dependencies.
func NewAgentManager(p Params) *AgentManager {
	return &AgentManager{
		tmux:        p.Tmux,
		sessions:    p.Store,
		selfPath:    p.SelfPath,
		claudeFlags: p.ClaudeFlags,
	}
}

// managerOpenFlag marks that the manager popup is currently open, so the
// binding can send prefix+g back to the picker instead of nesting a popup.
const managerOpenFlag = "@claude_manager_open"

// MarkPanelOpen and MarkPanelClosed toggle the popup-open flag.
func (m *AgentManager) MarkPanelOpen()   { m.tmux.SetGlobal(managerOpenFlag, "1") }
func (m *AgentManager) MarkPanelClosed() { m.tmux.SetGlobal(managerOpenFlag, "0") }

// PaneAlive reports whether an agent's tmux pane still exists, for pruning
// entries the panel lists from the service.
func (m *AgentManager) PaneAlive(pane string) bool {
	return m.tmux.PaneAlive(pane)
}

// Reconciler builds the level-triggered reconciler over this manager's tmux
// client and store, for the serve process to run on a timer.
func (m *AgentManager) Reconciler() *Reconciler {
	return NewReconciler(m.tmux, m.sessions)
}

// SessionAlive reports whether a tmux session with the given name still exists.
// Attaching targets a session by name, so this is what gates restoring it: a
// dormant chat's stored pane id can be empty or collide with a recycled id after
// a tmux server restart, both of which make PaneAlive lie.
func (m *AgentManager) SessionAlive(name string) bool {
	return m.tmux.HasSession(name)
}

// Preview sizes a detached agent's window to the preview box (so its capture
// fills the box), then returns a snapshot of its pane. Attached sessions are
// left alone so a live client's view is never disturbed.
func (m *AgentManager) Preview(agent store.Agent, width, height int) string {
	if width > 0 && height > 0 && !m.tmux.SessionAttached(agent.Name) {
		m.tmux.ResizeWindow(agent.Name, width, height)
	}

	return m.tmux.CapturePane(agent.Pane)
}

// ScrollPreview moves a live agent's own view by sending mouse-wheel events to
// its pane, so the next capture reflects the scroll. Claude keeps its scrollback
// internally and there is none in tmux to scroll locally. The wheel is aimed at
// the box center. Preview keeps the window sized to that box on every tick, so no
// resize is needed here. Attached sessions are left alone so a live client's view
// is never disturbed.
func (m *AgentManager) ScrollPreview(agent store.Agent, width, height int, up bool) {
	if agent.Pane == "" || width <= 0 || height <= 0 || m.tmux.SessionAttached(agent.Name) {
		return
	}

	m.tmux.ScrollPane(agent.Pane, width, height, up, previewScrollTicks)
}

// Rename retitles an agent, renaming both its tmux session (so state events
// keep keying to it) and its stored record.
func (m *AgentManager) Rename(agent store.Agent, name string) {
	name = store.SanitizeName(strings.TrimSpace(name))
	if name == "" || name == agent.Name {
		return
	}

	m.tmux.RenameSession(agent.Name, name)
	_ = m.sessions.Rename(context.Background(), agent.ID, name)
}

// Delete soft-deletes an agent's record (hiding it from the panel), stopping its
// session first.
func (m *AgentManager) Delete(client string, agent store.Agent) {
	if m.tmux.ClientSession(client) == agent.Name {
		m.tmux.SwitchToLast(client)
	}

	m.tmux.KillSession(agent.Name)
	_ = m.sessions.SoftDelete(context.Background(), agent.Name)
}

// NewSessionParams configures a background agent to create.
type NewSessionParams struct {
	Client string
	Title  string
	Dir    string
	Flags  string
	Prompt string
}

// NewSession creates a background session, records it so it lists immediately,
// and launches claude with the default plus per-session flags and opening
// prompt. It does not switch the client to the new session.
//
// The directory is the given Dir, else the client's current directory.
func (m *AgentManager) NewSession(p NewSessionParams) {
	dir := strings.TrimSpace(p.Dir)
	if dir == "" {
		dir = m.tmux.ClientPath(p.Client)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	name := m.uniqueSessionName(p.Title, p.Prompt)

	m.tmux.NewDetachedSession(name, dir)

	pane, _ := m.tmux.PaneID(name)
	flags := strings.TrimSpace(m.claudeFlags + " " + p.Flags)

	// Generate Claude's session id ourselves and launch with --session-id, so the
	// id is known without a hook reporting it, and restoring the agent can resume
	// that same conversation with --resume.
	sessionID := uuid.NewString()
	_ = m.sessions.Upsert(context.Background(), store.Agent{
		Name:            name,
		Pane:            pane,
		Cwd:             dir,
		Flags:           flags,
		Prompt:          p.Prompt,
		State:           store.StateNew,
		ClaudeSessionID: sessionID,
	})

	m.launchClaude(name, flags, p.Prompt, sessionID, false)
}

// SearchCandidates returns every tracked chat, including soft-deleted ones, for
// the search screen to index and match against.
func (m *AgentManager) SearchCandidates() ([]store.Agent, error) {
	agents, err := m.sessions.ListAll(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load search candidates: %w", err)
	}

	return agents, nil
}

// Restore recreates a dormant agent's tmux session from its stored cwd/flags
// and returns the new pane id, updating the stored record. A chat restored from
// search may be soft-deleted, so its deleted flag is cleared to return it to the
// normal listing.
func (m *AgentManager) Restore(agent store.Agent) (string, error) {
	dir := agent.Cwd
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	// A deleted agent's original name may have been reused by a live agent while
	// it sat deleted. Undeleting it under that name would collide on the
	// unique-name index and fail, so give it a fresh unique name first, renaming
	// the stored row by id. A non-deleted dormant agent already owns its name (its
	// own live row holds it), so it is left as is.
	if agent.Deleted && m.nameTaken(agent.Name) {
		newName := m.uniqueSessionName(agent.Name, "")
		if err := m.sessions.Rename(context.Background(), agent.ID, newName); err != nil {
			return "", fmt.Errorf("failed to rename restored session %q: %w", agent.Name, err)
		}

		agent.Name = newName
	}

	m.tmux.NewDetachedSession(agent.Name, dir)
	pane, _ := m.tmux.PaneID(agent.Name)

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

	m.launchClaude(agent.Name, agent.Flags, "", agent.ClaudeSessionID, true)

	return pane, nil
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

// nameTaken reports whether a name is unavailable for a new session. A name is
// taken if a live tmux session already uses it, or if a live agent record still
// owns it. The store check matters for a dormant agent, whose tmux session is
// gone yet whose record keeps the name. Reusing it would make the upsert clobber
// that agent's row instead of creating a fresh one.
func (m *AgentManager) nameTaken(name string) bool {
	if m.tmux.HasSession(name) {
		return true
	}

	taken, _ := m.sessions.ActiveNameExists(context.Background(), name)
	return taken
}

// launchClaude types the claude command into a session. When resume is set it
// resumes the prior conversation by session id; otherwise it starts a new
// conversation pinned to that session id, with the opening prompt passed through
// a temp file so multi-line input survives, removed as the command runs.
func (m *AgentManager) launchClaude(session, flags, prompt, sessionID string, resume bool) {
	command := "claude"
	if flags != "" {
		command += " " + flags
	}

	switch {
	case resume && sessionID != "":
		command += " --resume " + sessionID
	default:
		if sessionID != "" {
			command += " --session-id " + sessionID
		}
		if prompt != "" {
			file, err := os.CreateTemp("", "claude-prompt-*.md")
			if err == nil {
				_, _ = file.WriteString(prompt)
				_ = file.Close()
				command += fmt.Sprintf(" \"$(cat '%s'; rm '%s')\"", file.Name(), file.Name())
			}
		}
	}

	m.tmux.SendKeys(session, command, "Enter")
}

// Notify records that an agent finished. It upserts state only, so a
// tool-created agent keeps its captured cwd/flags/prompt.
func (m *AgentManager) Notify(mainSession, pane, _ string) {
	key := m.agentKey(mainSession)
	if key == "" {
		return
	}

	_ = m.sessions.UpdateState(context.Background(), key, pane, store.StateDone, "")
}

// Status renders the global agent summary for the tmux status bar.
func (m *AgentManager) Status() string {
	agents, err := m.sessions.List(context.Background())
	if err != nil {
		return ""
	}

	waiting, working, done := 0, 0, 0
	for _, agent := range agents {
		switch agent.State {
		case store.StateThinking:
			working++
		case store.StateWaiting:
			waiting++
		case store.StateDone:
			done++
		}
	}

	const base = "#[fg=#7E9CD8]"

	var segments []string
	if waiting > 0 {
		segments = append(segments, fmt.Sprintf("#[fg=#E82424]%d waiting%s", waiting, base))
	}
	if working > 0 {
		segments = append(segments, fmt.Sprintf("#[fg=#FF9E3B]%d working%s", working, base))
	}
	if done > 0 {
		segments = append(segments, fmt.Sprintf("#[fg=#98BB6C]%d done%s", done, base))
	}
	if len(segments) == 0 {
		return ""
	}

	return strings.Join(segments, "  ") + "  "
}

// AttachSession attaches a tmux client to the agent's session, focused on its
// pane, and blocks until it detaches.
func (m *AgentManager) AttachSession(session, pane string) error {
	m.tmux.SetWindowSizeLatest(session)

	// Run (not exec) so control returns to the panel loop when the client
	// detaches. TMUX is stripped so tmux does not refuse the attach as nested.
	cmd := exec.Command("tmux", "-L", m.tmux.Socket(), "attach-session", "-t", session, ";", "select-pane", "-t", pane)
	cmd.Env = envWithout("TMUX")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to attach to session %q: %w", session, err)
	}

	return nil
}

func (m *AgentManager) agentKey(mainSession string) string {
	if mainSession != "" {
		return mainSession
	}

	session, _ := m.tmux.CurrentSession()
	return session
}

func envWithout(key string) []string {
	prefix := key + "="
	full := os.Environ()
	filtered := make([]string, 0, len(full))
	for _, entry := range full {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}
