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
	"syscall"
	"time"

	"claude-agents/internal/store"
	"claude-agents/internal/tmux"
	"claude-agents/lib/xstr"
)

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

// Jump switches the client to an agent's session and focuses its pane, first
// restoring client-fit sizing so the agent fills the real terminal.
func (m *AgentManager) Jump(client string, agent store.Agent) {
	m.tmux.SetWindowSizeLatest(agent.Name)
	location := m.tmux.PaneLocation(agent.Pane)
	m.tmux.SwitchClient(client, agent.Name)
	if location != "" {
		m.tmux.SelectWindow(location)
		m.tmux.SelectPane(agent.Pane)
	}
}

// Chat schedules the large chat popup to open once the panel popup has closed.
func (m *AgentManager) Chat(agent store.Agent) {
	m.tmux.RunShellBackground(fmt.Sprintf("%s chat-open '%s' '%s'", m.selfPath, agent.Name, agent.Pane))
}

// Last bounces the client to its previous session.
func (m *AgentManager) Last(client string) {
	m.tmux.SwitchToLast(client)
}

// Destroy kills an agent's session, bouncing the client away first if it is
// currently viewing it, and drops the stored row.
func (m *AgentManager) Destroy(client string, agent store.Agent) {
	if m.tmux.ClientSession(client) == agent.Name {
		m.tmux.SwitchToLast(client)
	}

	m.tmux.KillSession(agent.Name)
	_ = m.sessions.Delete(context.Background(), agent.Name)
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

	m.launchClaude(name, flags, p.Prompt)

	m.runHook(m.hooks.PostCreate, name, dir)
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

// launchClaude types the claude command into a session. The prompt is passed
// through a temp file so multi-line input survives, and the file is removed as
// the command runs.
func (m *AgentManager) launchClaude(session, flags, prompt string) {
	command := "claude"
	if flags != "" {
		command += " " + flags
	}

	if prompt != "" {
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

	_ = m.sessions.UpsertState(context.Background(), key, pane, store.StateDone)
}

// Hook advances an agent's state from a Claude Code lifecycle event.
func (m *AgentManager) Hook(mode, mainSession, pane string) {
	key := m.agentKey(mainSession)
	if key == "" {
		return
	}

	ctx := context.Background()
	switch mode {
	case "prompt":
		_ = m.sessions.UpsertState(ctx, key, pane, store.StateThinking)
	case "stop":
		existing, _ := m.sessions.Get(ctx, key)
		if existing != nil && existing.State == store.StateDone {
			return
		}

		_ = m.sessions.UpsertState(ctx, key, pane, store.StateDone)
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

// OpenChatPopup opens the large chat popup on the attached client. It runs
// detached from the panel, so it first waits for the panel popup to close.
func (m *AgentManager) OpenChatPopup(session, pane string) {
	time.Sleep(200 * time.Millisecond)

	client := m.tmux.FirstClient()
	if client == "" {
		return
	}

	m.tmux.DisplayPopup(client, "90%", "90%", fmt.Sprintf("%s chat '%s' '%s'", m.selfPath, session, pane))
}

// AttachChat replaces this process with a tmux client attached to the agent's
// session and focused on its pane. TMUX is stripped so tmux does not refuse the
// attach as nested.
func (m *AgentManager) AttachChat(session, pane string) error {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("failed to find tmux: %w", err)
	}

	m.tmux.SetWindowSizeLatest(session)

	argv := []string{"tmux", "-L", m.tmux.Socket(), "attach-session", "-t", session, ";", "select-pane", "-t", pane}
	if err := syscall.Exec(path, argv, envWithout("TMUX")); err != nil {
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
