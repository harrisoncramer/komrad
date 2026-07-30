package ui

import (
	"context"
	"fmt"
	"strings"

	"claude-agents/internal/appconfig"
	"claude-agents/lib/dates"
	agentv1 "claude-agents/proto/gen/agent/v1"

	"connectrpc.com/connect"

	agentstore "claude-agents/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// enterTrash switches to the trash, the deleted agents. It is its own screen
// rather than a section of the agents tree because a deleted agent is not
// something to act on alongside live work.
func (m model) enterTrash() (tea.Model, tea.Cmd) {
	m.screen = screenTrash
	m.stageTrash()

	return m, m.refreshCmd()
}

// deletedAgentsFrom renders listed agents into the store type the screen is
// written against. Liveness is not carried: archiving kills the pane, so nothing
// in the trash is running.
func deletedAgentsFrom(listed []*agentv1.Agent) []agentstore.Agent {
	agents := make([]agentstore.Agent, 0, len(listed))
	for _, agent := range listed {
		agents = append(agents, protoToAgent(agent))
	}

	return agents
}

// applyTrash takes a freshly-listed set as the on-screen trash, keeping the
// cursor on the agent it was on rather than on its index, so a row removed above
// it does not move the selection onto something else.
func (m *model) applyTrash(fresh []agentstore.Agent) {
	selected := ""
	if agent, ok := m.selectedTrash(); ok {
		selected = agent.ID
	}

	m.trash = fresh

	for i, agent := range m.trash {
		if agent.ID == selected {
			m.trashCursor = i

			return
		}
	}

	m.trashCursor = clampCursor(m.trashCursor, len(m.trash))
}

// updateTrash drives the trash. Nav moves the selection and restages, restore
// puts the hovered agent back in the panel, and delete asks before destroying it.
func (m model) updateTrash(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case bound(k, m.keys.Trash.Quit):
		return m, m.hide()
	case bound(k, m.keys.Trash.Back):
		return m.returnToAgents()
	case bound(k, m.keys.Nav.Up):
		if m.trashCursor > 0 {
			m.trashCursor--
			m.stageTrash()
		}
	case bound(k, m.keys.Nav.Down):
		if m.trashCursor < len(m.trash)-1 {
			m.trashCursor++
			m.stageTrash()
		}
	case bound(k, m.keys.Trash.Restore):
		return m.restoreFromTrash()
	case bound(k, m.keys.Trash.Delete):
		return m.deleteFromTrashConfirm()
	}

	return m, nil
}

// selectedTrash returns the deleted agent under the cursor, or false when the
// trash is empty.
func (m model) selectedTrash() (agentstore.Agent, bool) {
	if m.trashCursor < 0 || m.trashCursor >= len(m.trash) {
		return agentstore.Agent{}, false
	}

	return m.trash[m.trashCursor], true
}

// restoreFromTrash brings the hovered agent back. Restoring relaunches claude on
// the same conversation, so the agent returns running rather than as a row, and
// it may come back renamed if a live agent has taken its name. It leaves the
// trash either way, so the list is re-read.
func (m model) restoreFromTrash() (tea.Model, tea.Cmd) {
	agent, ok := m.selectedTrash()
	if !ok {
		return m, nil
	}

	return m, m.restoreCmd(fmt.Sprintf("Could not restore agent %q", agent.Name), agent.ID, screenTrash)
}

// deleteFromTrashConfirm opens the prompt for destroying the hovered agent. This
// is the one action in the panel nothing undoes, so it is the only per-row
// action that asks first.
func (m model) deleteFromTrashConfirm() (tea.Model, tea.Cmd) {
	agent, ok := m.selectedTrash()
	if !ok {
		return m, nil
	}

	m.trashDeleteTarget = &agent
	m.form, m.confirmed = deleteAgentForm(agent.Name, m.keys.Form)
	m.screen = screenTrashDelete

	return m, m.form.Init()
}

// updateTrashDelete drives the delete prompt. Confirming destroys the agent,
// declining and aborting leave it in the trash, and all three return to the
// trash rather than to the agents list, since that is where the key was pressed.
// Re-entering re-reads the list, so a destroyed agent is gone from it.
func (m model) updateTrashDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		var write tea.Cmd
		if target := m.trashDeleteTarget; target != nil && m.wasConfirmed() {
			write = m.writeCmd(
				fmt.Sprintf("Could not delete agent %q", target.Name),
				func(a api) error { return a.deleteAgent(target.ID) },
			)
		}
		m.trashDeleteTarget = nil
		m.confirmed = nil
		entered, refresh := m.enterTrash()

		return entered, tea.Batch(write, refresh)
	case huh.StateAborted:
		m.trashDeleteTarget = nil
		m.confirmed = nil
		return m.enterTrash()
	default:
		return m, cmd
	}
}

// deleteAgent destroys an agent for good, keyed by id. A name identifies at most
// one live agent, which is exactly what a deleted one is not.
func (a api) deleteAgent(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := a.agents.DeleteAgent(ctx, connect.NewRequest(&agentv1.DeleteAgentRequest{Id: id}))

	return err
}

// deleteAgentForm confirms destroying an agent. It names what goes with it,
// since the reversible delete on the agents list uses the same key and this one
// cannot be taken back.
func deleteAgentForm(name string, keys appconfig.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Permanently delete agent %q?", name),
		"Its transcript goes with it and search will not find the conversation again.",
		keys,
	)
}

// stageTrash shows the hovered agent's stored record in the stage. Nothing in
// the trash is running, so there is never a pane to swap in.
func (m model) stageTrash() {
	agent, ok := m.selectedTrash()
	if !ok {
		m.stage.Text(emptyTrashMarkdown)
		return
	}

	m.stage.Text(trashMarkdown(agent, m.scheduleName(agent)))
}

// emptyTrashMarkdown is what the stage shows with nothing in the trash, so the
// pane says why it is blank rather than sitting empty.
const emptyTrashMarkdown = "# Trash\n\n_nothing deleted_\n\nAgents deleted from the panel are kept here until you destroy them.\n"

// trashMarkdown renders a deleted agent's stored record for the stage. It is the
// same record the agents list shows, minus the liveness, plus what the two keys
// here would do to it.
func trashMarkdown(agent agentstore.Agent, schedule string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", agent.Name)
	b.WriteString("_deleted — kept until destroyed_\n\n")
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- directory: %s\n", orDash(agent.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(agent.Flags))
	fmt.Fprintf(&b, "- resume: %s\n", orDash(agent.ClaudeSessionID))
	if schedule != "" {
		fmt.Fprintf(&b, "- launched by: %s\n", schedule)
	}
	if created := dates.Friendly(agent.CreatedAt); created != "" {
		fmt.Fprintf(&b, "- created: %s\n", created)
	}
	b.WriteString("\nRestore brings it back and picks the conversation up where it stopped.\n")

	return b.String()
}

// trashView renders the trash's own pane, the list of deleted agents.
func (m model) trashView() string {
	header := headerStyle.Render("Trash") + "\n\n"
	hints := "\n" + m.trashHints()

	if len(m.trash) == 0 {
		return header + dimStyle.Render("nothing deleted") + "\n" + hints
	}

	rows := make([]string, 0, len(m.trash))
	for i, agent := range m.trash {
		prefix := "  "
		name := dimStyle.Render(agent.Name)
		if i == m.trashCursor {
			prefix = cursorStyle.Render("› ")
			name = cursorStyle.Render(agent.Name)
		}

		rows = append(rows, prefix+name+scheduledMark(agent)+markGlyph(agent, m.marks))
	}

	return header + renderList(rows, m.trashCursor, m.listLines(header, hints)) + hints
}

// trashHints lists the trash screen's bindings.
func (m model) trashHints() string {
	return renderHints([]hint{
		{hintKeys(m.keys.Trash.Restore), "restore"},
		{hintKeys(m.keys.Trash.Delete), "delete for good"},
		{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		{hintKeys(m.keys.Trash.Back), "back"},
	})
}
