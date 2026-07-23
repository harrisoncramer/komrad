// Package agents orchestrates the store and tmux to implement both the panel
// actions and the CLI subcommands (notify, status, hook, chat).
package agents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"claude-agents/internal/store"
	"claude-agents/internal/tmux"
	"claude-agents/lib/xstr"
)

// previewScrollTicks is how many wheel notches one preview scroll sends, so a
// keypress moves the agent's view a noticeable amount.
const previewScrollTicks = 4

// Hooks are paths to lifecycle scripts, each optional.
type Hooks struct {
	PreCreate  string
	PostCreate string
	OnDelete   string
}

// AgentManager lists, launches, and acts on Claude agents by coordinating a
// tmux client and the SQLite-backed store.
type AgentManager struct {
	tmux        *tmux.Client
	sessions    *store.Store
	selfPath    string
	claudeFlags string
	hooks       Hooks
}

// Params carries the AgentManager's dependencies.
type Params struct {
	Tmux        *tmux.Client
	Store       *store.Store
	SelfPath    string
	ClaudeFlags string
	Hooks       Hooks
}

// NewAgentManager constructs an AgentManager from its dependencies.
func NewAgentManager(p Params) *AgentManager {
	return &AgentManager{
		tmux:        p.Tmux,
		sessions:    p.Store,
		selfPath:    p.SelfPath,
		claudeFlags: p.ClaudeFlags,
		hooks:       p.Hooks,
	}
}

// runHook runs a lifecycle hook script (when configured) with the given args
// and returns its trimmed stdout. A hook is best effort; failures are ignored.
func (m *AgentManager) runHook(path string, args ...string) string {
	if path == "" {
		return ""
	}

	out, err := exec.Command(expandHome(path), args...).Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}

	return path
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
// the box center; Preview keeps the window sized to that box on every tick, so no
// resize is needed here. Attached sessions are left alone so a live client's view
// is never disturbed.
func (m *AgentManager) ScrollPreview(agent store.Agent, width, height int, up bool) {
	if agent.Pane == "" || width <= 0 || height <= 0 || m.tmux.SessionAttached(agent.Name) {
		return
	}

	m.tmux.ScrollPane(agent.Pane, width, height, up, previewScrollTicks)
}

// Rename retitles an agent, renaming both its tmux session (so hooks keep
// keying to it) and its stored record.
func (m *AgentManager) Rename(agent store.Agent, name string) {
	name = store.SanitizeName(strings.TrimSpace(name))
	if name == "" || name == agent.Name {
		return
	}

	m.tmux.RenameSession(agent.Name, name)
	_ = m.sessions.Rename(context.Background(), agent.ID, name)
}

// Delete soft-deletes an agent's record (hiding it from the panel), stopping its
// session first and running the on-delete hook.
func (m *AgentManager) Delete(client string, agent store.Agent) {
	if m.tmux.ClientSession(client) == agent.Name {
		m.tmux.SwitchToLast(client)
	}

	m.tmux.KillSession(agent.Name)
	_ = m.sessions.SoftDelete(context.Background(), agent.Name)
	m.runHook(m.hooks.OnDelete, agent.Name, "")
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
// The directory is the given Dir, else the client's current directory. A
// configured pre-create hook runs first with the session name and directory and
// may print the directory to actually use (e.g. a freshly created worktree).
func (m *AgentManager) NewSession(p NewSessionParams) {
	dir := strings.TrimSpace(p.Dir)
	if dir == "" {
		dir = m.tmux.ClientPath(p.Client)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	name := m.uniqueSessionName(p.Title, p.Prompt)

	if provisioned := m.runHook(m.hooks.PreCreate, name, dir); provisioned != "" {
		dir = provisioned
	}

	m.tmux.NewDetachedSession(name, dir)

	pane, _ := m.tmux.PaneID(name)
	flags := strings.TrimSpace(m.claudeFlags + " " + p.Flags)
	_ = m.sessions.Upsert(context.Background(), store.Agent{
		Name:   name,
		Pane:   pane,
		Cwd:    dir,
		Flags:  flags,
		Prompt: p.Prompt,
		State:  store.StateNew,
	})

	m.launchClaude(name, flags, p.Prompt, "")

	m.runHook(m.hooks.PostCreate, name, dir)
}

// Restore recreates a dormant agent's tmux session from its stored cwd/flags
// and returns the new pane id, updating the stored record.
func (m *AgentManager) Restore(agent store.Agent) (string, error) {
	dir := agent.Cwd
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	m.tmux.NewDetachedSession(agent.Name, dir)
	pane, _ := m.tmux.PaneID(agent.Name)

	agent.Pane = pane
	agent.State = store.StateNew
	if err := m.sessions.Upsert(context.Background(), agent); err != nil {
		return "", fmt.Errorf("failed to restore session %q: %w", agent.Name, err)
	}

	m.launchClaude(agent.Name, agent.Flags, "", agent.ClaudeSessionID)

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
	for i := 2; m.tmux.HasSession(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}

	return name
}

// launchClaude types the claude command into a session. A resumeID resumes that
// prior conversation; otherwise an opening prompt is passed through a temp file
// (so multi-line input survives), removed as the command runs.
func (m *AgentManager) launchClaude(session, flags, prompt, resumeID string) {
	command := "claude"
	if flags != "" {
		command += " " + flags
	}

	switch {
	case resumeID != "":
		command += " --resume " + resumeID
	case prompt != "":
		file, err := os.CreateTemp("", "claude-prompt-*.md")
		if err == nil {
			_, _ = file.WriteString(prompt)
			_ = file.Close()
			command += fmt.Sprintf(" \"$(cat '%s'; rm '%s')\"", file.Name(), file.Name())
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

	_ = m.sessions.UpsertState(context.Background(), key, pane, store.StateDone, "")
}

// Hook advances an agent's state from a Claude Code lifecycle event and records
// Claude's own session id (for --resume on restore).
func (m *AgentManager) Hook(mode, mainSession, pane, claudeSessionID string) {
	key := m.agentKey(mainSession)
	if key == "" {
		return
	}

	ctx := context.Background()
	switch mode {
	case "prompt":
		_ = m.sessions.UpsertState(ctx, key, pane, store.StateThinking, claudeSessionID)
	case "stop":
		existing, _ := m.sessions.Get(ctx, key)
		if existing != nil && existing.State == store.StateDone {
			return
		}

		_ = m.sessions.UpsertState(ctx, key, pane, store.StateDone, claudeSessionID)
	}
}

// Status renders the global agent summary for the tmux status bar.
func (m *AgentManager) Status() string {
	agents, err := m.sessions.List(context.Background())
	if err != nil {
		return ""
	}

	working, done := 0, 0
	for _, agent := range agents {
		switch agent.State {
		case store.StateThinking:
			working++
		case store.StateDone:
			done++
		}
	}

	const base = "#[fg=#7E9CD8]"

	var out string
	if working > 0 {
		out = fmt.Sprintf("#[fg=#FF9E3B]%d working%s", working, base)
	}
	if done > 0 {
		if out != "" {
			out += "  "
		}

		out += fmt.Sprintf("#[fg=#E82424]%d done%s", done, base)
	}
	if out == "" {
		return ""
	}

	return out + "  "
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
