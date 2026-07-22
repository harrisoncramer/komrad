package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"claude-panel/config"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	socket    = "default"
	agentsDir string
)

func tmux(args ...string) (string, error) {
	out, err := exec.Command("tmux", append([]string{"-L", socket}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// runTmux executes a best-effort tmux side effect. A failure is not actionable
// here: the panel closes immediately after, so there is nothing to recover.
func runTmux(args ...string) {
	_, _ = tmux(args...)
}

func clientArgs(client string) []string {
	if client == "" {
		return nil
	}
	return []string{"-c", client}
}

func registryDir() string {
	return agentsDir
}

type agent struct {
	file, session, pane, state, msg string
}

func paneAlive(pane string) bool {
	if pane == "" {
		return true
	}
	out, err := tmux("display-message", "-p", "-t", pane, "#{pane_id}")
	return err == nil && out == pane
}

// loadAgents reads the registry, dropping any entry whose pane has gone away.
func loadAgents() []agent {
	entries, _ := os.ReadDir(registryDir())
	var agents []agent
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(registryDir(), entry.Name())
		contents, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		ag := agent{file: path}
		for _, line := range strings.Split(string(contents), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			switch key {
			case "SESSION":
				ag.session = value
			case "PANE":
				ag.pane = value
			case "STATE":
				ag.state = value
			case "MSG":
				ag.msg = value
			}
		}
		if !paneAlive(ag.pane) {
			_ = os.Remove(path)
			continue
		}
		agents = append(agents, ag)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].session < agents[j].session })
	return agents
}

func sanitizeSessionName(name string) string {
	replacer := strings.NewReplacer(":", "-", ".", "-", " ", "-")
	return replacer.Replace(name)
}

func jump(client string, ag agent) {
	location, _ := tmux("display-message", "-p", "-t", ag.pane, "#{session_name}:#{window_index}")
	runTmux(append(append([]string{"switch-client"}, clientArgs(client)...), "-t", ag.session)...)
	if location != "" {
		runTmux("select-window", "-t", location)
		runTmux("select-pane", "-t", ag.pane)
	}
}

func chat(scriptsDir string, ag agent) {
	open := filepath.Join(scriptsDir, "claude-chat-open")
	runTmux("run-shell", "-b", fmt.Sprintf("%s '%s' '%s'", open, ag.session, ag.pane))
}

func lastSession(client string) {
	runTmux(append(append([]string{"switch-client"}, clientArgs(client)...), "-l")...)
}

// destroy kills the agent's session. If the client sits in that session it is
// bounced to the previous one first so it is not dropped.
func destroy(client string, ag agent) {
	current, _ := tmux(append([]string{"display-message", "-p"}, append(clientArgs(client), "#{session_name}")...)...)
	if current == ag.session {
		lastSession(client)
	}

	runTmux("kill-session", "-t", ag.session)
	_ = os.Remove(ag.file)
}

// newChat creates a named session in the current terminal's directory, registers
// it so it lists immediately, launches claude in it, and switches to it.
func newChat(client, name string) {
	name = sanitizeSessionName(name)
	dir, _ := tmux(append([]string{"display-message", "-p"}, append(clientArgs(client), "#{pane_current_path}")...)...)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	if _, err := tmux("has-session", "-t", name); err != nil {
		runTmux("new-session", "-d", "-s", name, "-c", dir)
		pane, _ := tmux("display-message", "-p", "-t", name, "#{pane_id}")
		entry := fmt.Sprintf("SESSION=%s\nPANE=%s\nSTATE=new\nMSG=\n", name, pane)
		_ = os.WriteFile(filepath.Join(registryDir(), sanitizeSessionName(name)), []byte(entry), 0o644)
		runTmux("send-keys", "-t", name, "claude", "Enter")
	}

	runTmux(append(append([]string{"switch-client"}, clientArgs(client)...), "-t", name)...)
}

var (
	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#727169"))
)

func stateIcon(state string) string {
	switch state {
	case "done":
		return "✓"
	case "thinking":
		return "…"
	default:
		return "•"
	}
}

func stateColor(state string) lipgloss.Color {
	switch state {
	case "done":
		return lipgloss.Color("#E82424")
	case "thinking":
		return lipgloss.Color("#FF9E3B")
	default:
		return lipgloss.Color("#7E9CD8")
	}
}

type screen int

const (
	screenList screen = iota
	screenAction
	screenName
	screenConfirm
)

const (
	rowNewChat = "new"
	rowLast    = "last"
	rowAgent   = "agent"
)

type row struct {
	kind  string
	label string
	agent agent
}

type model struct {
	client      string
	scriptsDir  string
	screen      screen
	rows        []row
	cursor      int
	agent       agent
	actions     []string
	actionIndex int
	nameInput   textinput.Model
	confirmYes  bool
}

func buildRows(agents []agent) []row {
	rows := []row{
		{kind: rowNewChat, label: "+  new chat"},
		{kind: rowLast, label: "←  last window/session"},
	}
	for _, ag := range agents {
		rows = append(rows, row{kind: rowAgent, agent: ag})
	}
	return rows
}

func newModel(client, scriptsDir string) model {
	input := textinput.New()
	input.Placeholder = "new session name"
	input.CharLimit = 60
	input.Width = 40

	return model{
		client:     client,
		scriptsDir: scriptsDir,
		screen:     screenList,
		rows:       buildRows(loadAgents()),
		nameInput:  input,
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch m.screen {
	case screenList:
		return m.updateList(key)
	case screenAction:
		return m.updateAction(key)
	case screenName:
		return m.updateName(key)
	case screenConfirm:
		return m.updateConfirm(key)
	}
	return m, nil
}

func (m model) updateList(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "enter":
		selected := m.rows[m.cursor]
		switch selected.kind {
		case rowNewChat:
			m.screen = screenName
			m.nameInput.Focus()
			return m, textinput.Blink
		case rowLast:
			lastSession(m.client)
			return m, tea.Quit
		case rowAgent:
			m.agent = selected.agent
			m.actions = []string{"jump", "chat", "destroy"}
			m.actionIndex = 0
			m.screen = screenAction
		}
	}
	return m, nil
}

func (m model) updateAction(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "left", "h":
		m.screen = screenList
	case "up", "k":
		if m.actionIndex > 0 {
			m.actionIndex--
		}
	case "down", "j":
		if m.actionIndex < len(m.actions)-1 {
			m.actionIndex++
		}
	case "enter":
		switch m.actions[m.actionIndex] {
		case "jump":
			jump(m.client, m.agent)
			return m, tea.Quit
		case "chat":
			chat(m.scriptsDir, m.agent)
			return m, tea.Quit
		case "destroy":
			m.confirmYes = false
			m.screen = screenConfirm
		}
	}
	return m, nil
}

func (m model) updateName(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.screen = screenList
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.nameInput.Value())
		if name == "" {
			return m, tea.Quit
		}
		newChat(m.client, name)
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(key)
	return m, cmd
}

func (m model) updateConfirm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "esc", "n":
		return m, tea.Quit
	case "left", "right", "h", "l", "tab":
		m.confirmYes = !m.confirmYes
	case "y":
		destroy(m.client, m.agent)
		return m, tea.Quit
	case "enter":
		if m.confirmYes {
			destroy(m.client, m.agent)
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() string {
	switch m.screen {
	case screenName:
		return headerStyle.Render("new chat") + "\n\n  " + m.nameInput.View() +
			"\n\n" + dimStyle.Render("  enter create · esc back")
	case screenConfirm:
		yes, no := "  Yes  ", "  No  "
		if m.confirmYes {
			yes = cursorStyle.Render("▸ Yes ")
		} else {
			no = cursorStyle.Render("▸ No ")
		}
		return headerStyle.Render("destroy") + "\n\n  Kill session '" + m.agent.session + "'?" +
			"\n\n  " + yes + "   " + no + "\n"
	case screenAction:
		var out strings.Builder
		out.WriteString(headerStyle.Render(m.agent.session) + "\n\n")
		for i, action := range m.actions {
			out.WriteString(m.renderRow(action, i == m.actionIndex))
		}
		out.WriteString("\n" + dimStyle.Render("enter select · esc back"))
		return out.String()
	default:
		var out strings.Builder
		out.WriteString(headerStyle.Render("Claude agents") + "\n\n")
		for i, r := range m.rows {
			out.WriteString(m.renderRow(m.rowLabel(r), i == m.cursor))
		}
		out.WriteString("\n" + dimStyle.Render("enter select · q quit"))
		return out.String()
	}
}

func (m model) rowLabel(r row) string {
	if r.kind != rowAgent {
		return r.label
	}

	icon := lipgloss.NewStyle().Foreground(stateColor(r.agent.state)).Render(stateIcon(r.agent.state))
	label := icon + "  " + r.agent.session
	if r.agent.msg != "" {
		label += dimStyle.Render("  —  " + r.agent.msg)
	}
	return label
}

func (m model) renderRow(label string, selected bool) string {
	cursor := "  "
	if selected {
		cursor = cursorStyle.Render("› ")
	}
	return cursor + label + "\n"
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	socket = cfg.TmuxSocket
	agentsDir = cfg.AgentsDir

	client := ""
	if len(os.Args) > 1 {
		client = os.Args[1]
	}

	executable, _ := os.Executable()
	scriptsDir := filepath.Join(filepath.Dir(executable), "..", "scripts")

	if _, err := tea.NewProgram(newModel(client, scriptsDir)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
