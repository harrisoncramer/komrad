// Package agents orchestrates the registry and tmux to implement both the panel
// actions and the CLI subcommands (notify, status, hook, chat).
package agents

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"claude-agents/internal/registry"
	"claude-agents/internal/tmux"
	"claude-agents/lib/xstr"
)

// AgentManager lists, launches, and acts on Claude agents by coordinating a
// tmux client and the agent store.
type AgentManager struct {
	tmux     *tmux.Client
	store    *registry.Store
	selfPath string
}

// Params carries the AgentManager's dependencies.
type Params struct {
	Tmux     *tmux.Client
	Store    *registry.Store
	SelfPath string
}

// NewAgentManager constructs an AgentManager from its dependencies.
func NewAgentManager(p Params) *AgentManager {
	return &AgentManager{
		tmux:     p.Tmux,
		store:    p.Store,
		selfPath: p.SelfPath,
	}
}

// List returns the live agents, pruning any whose pane has gone away.
func (m *AgentManager) List() []registry.Agent {
	all, err := m.store.List()
	if err != nil {
		return nil
	}

	live := make([]registry.Agent, 0, len(all))
	for _, agent := range all {
		if !m.tmux.PaneAlive(agent.Pane) {
			_ = m.store.Remove(agent)
			continue
		}

		live = append(live, agent)
	}

	return live
}

// Jump switches the client to an agent's session and focuses its pane.
func (m *AgentManager) Jump(client string, agent registry.Agent) {
	location := m.tmux.PaneLocation(agent.Pane)
	m.tmux.SwitchClient(client, agent.Session)
	if location != "" {
		m.tmux.SelectWindow(location)
		m.tmux.SelectPane(agent.Pane)
	}
}

// Chat schedules the large chat popup to open once the panel popup has closed.
func (m *AgentManager) Chat(agent registry.Agent) {
	m.tmux.RunShellBackground(fmt.Sprintf("%s chat-open '%s' '%s'", m.selfPath, agent.Session, agent.Pane))
}

// Last bounces the client to its previous session.
func (m *AgentManager) Last(client string) {
	m.tmux.SwitchToLast(client)
}

// Destroy kills an agent's session, bouncing the client away first if it is
// currently viewing it, and drops the registry entry.
func (m *AgentManager) Destroy(client string, agent registry.Agent) {
	if m.tmux.ClientSession(client) == agent.Session {
		m.tmux.SwitchToLast(client)
	}

	m.tmux.KillSession(agent.Session)
	_ = m.store.Remove(agent)
}

// NewSessionParams configures a background agent to create.
type NewSessionParams struct {
	Client string
	Title  string
	Flags  string
	Prompt string
}

// NewSession creates a background session in the client's current directory,
// registers it so it lists immediately, and launches claude with the given
// flags and opening prompt. It does not switch the client to the new session.
func (m *AgentManager) NewSession(p NewSessionParams) {
	dir := m.tmux.ClientPath(p.Client)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	name := m.uniqueSessionName(p.Title, p.Prompt)
	m.tmux.NewDetachedSession(name, dir)

	pane, _ := m.tmux.PaneID(name)
	summary := xstr.Truncate(strings.TrimSpace(xstr.FirstLine(p.Prompt)), 60)
	_ = m.store.Write(registry.Agent{Session: name, Pane: pane, State: registry.StateNew, Msg: summary})

	m.launchClaude(name, p.Flags, p.Prompt)
}

// uniqueSessionName derives a session name from the title, falling back to a
// slug of the prompt, then "agent", and appends a suffix until it is free.
func (m *AgentManager) uniqueSessionName(title, prompt string) string {
	base := registry.SanitizeSessionName(strings.TrimSpace(title))
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

// Notify records that an agent finished with a custom message.
func (m *AgentManager) Notify(mainSession, pane, msg string) {
	key := m.agentKey(mainSession)
	if key == "" {
		return
	}

	if msg == "" {
		msg = "Task complete."
	}

	_ = m.store.Write(registry.Agent{Session: key, Pane: pane, State: registry.StateDone, Msg: msg})
}

// Hook advances an agent's state from a Claude Code lifecycle event.
func (m *AgentManager) Hook(mode, mainSession, pane string) {
	key := m.agentKey(mainSession)
	if key == "" {
		return
	}

	switch mode {
	case "prompt":
		_ = m.store.Write(registry.Agent{Session: key, Pane: pane, State: registry.StateThinking})
	case "stop":
		existing, _ := m.store.Get(key)
		if existing != nil && existing.State == registry.StateDone {
			return
		}

		_ = m.store.Write(registry.Agent{Session: key, Pane: pane, State: registry.StateDone})
	}
}

// Status renders the global agent summary for the tmux status bar.
func (m *AgentManager) Status() string {
	agents, err := m.store.List()
	if err != nil {
		return ""
	}

	working, done := 0, 0
	for _, agent := range agents {
		switch agent.State {
		case registry.StateThinking:
			working++
		case registry.StateDone:
			done++
		case registry.StateNew:
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

	argv := []string{"tmux", "-L", m.socket(), "attach-session", "-t", session, ";", "select-pane", "-t", pane}
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

// socket exposes the tmux socket the client uses for the attach exec.
func (m *AgentManager) socket() string {
	return m.tmux.Socket()
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
