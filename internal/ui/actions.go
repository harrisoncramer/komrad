package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"weave/internal/actions"
	"weave/internal/errui"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// actionState is the actions screen, the scripts you have written for the panel to run.
// The list is read on the way in rather than polled, since it is a directory read and the
// screen is only up for as long as it takes to pick one.
//
// agent is the agent the screen was opened on, held here rather than read back off the cursor
// when a script is chosen. The refresh keeps running while the list is up and can move the
// rows under it, so reading the cursor then could run the script against whatever the last
// pass left there. It is a snapshot either way, so a script that cares whether the agent is
// still there asks the service.
type actionState struct {
	items  []actions.Action
	cursor int
	agent  agentstore.Agent
	alive  bool
}

// enterActions opens the actions list on the hovered agent. The directory is read on each
// press, so a script written since the panel started is there without a restart, and one
// deleted since is gone.
func (m model) enterActions() (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	found, err := actions.Discover(m.actionsDir)
	if err != nil {
		m.report("Could not read your actions", err)

		return m, nil
	}

	m.actions = actionState{items: found, agent: item.agent, alive: item.alive}
	m.screen = screenActions
	m.stageAction()

	return m, nil
}

// updateActions drives the actions list. Nav moves the selection and restages, and run
// hands the agent to the script under the cursor.
func (m model) updateActions(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case bound(k, m.keys.Actions.Quit):
		return m, m.hide()
	case bound(k, m.keys.Actions.Back):
		return m.returnToAgents()
	case bound(k, m.keys.Nav.Up):
		if m.actions.cursor > 0 {
			m.actions.cursor--
			m.stageAction()
		}
	case bound(k, m.keys.Nav.Down):
		if m.actions.cursor < len(m.actions.items)-1 {
			m.actions.cursor++
			m.stageAction()
		}
	case bound(k, m.keys.Actions.Run):
		return m.runSelectedAction()
	}

	return m, nil
}

// selectedAction returns the script under the cursor, or false when there are none.
func (m model) selectedAction() (actions.Action, bool) {
	if m.actions.cursor < 0 || m.actions.cursor >= len(m.actions.items) {
		return actions.Action{}, false
	}

	return m.actions.items[m.actions.cursor], true
}

// runSelectedAction starts the script under the cursor on the agent the list was opened on.
// Nothing waits for it and nothing reports what it did, since where its output goes is the
// script's to decide, and whatever it changes reaches the panel the way a change made from
// anywhere else does, on the next refresh.
//
// The list stays where it is. Firing an action is closer to a webhook than to opening
// something, so there is nothing to navigate to, and staying put leaves the cursor on the
// agent to fire a second action at.
//
// A script that would not start is reported, since the panel is the only thing that can say
// so, and the row wanting chmod is on the screen the banner sits over.
func (m model) runSelectedAction() (tea.Model, tea.Cmd) {
	action, ok := m.selectedAction()
	if !ok {
		return m, nil
	}

	failure := fmt.Sprintf("Could not run action %q", action.Name)

	env, err := m.actionEnv()
	if err != nil {
		m.report(failure, err)

		return m, nil
	}

	if err := m.runner.Run(action, m.actions.agent.Cwd, env); err != nil {
		m.report(failure, err)

		return m, nil
	}

	// A previous refusal on this screen is cleared, so the banner cannot outlive the
	// chmod that fixed it.
	m.report(failure, nil)

	return m, nil
}

// actionPayload is what an action is told about the agent it was run on, encoded into
// WEAVE_AGENT. It is a shape of its own rather than the wire format, so what a script
// parses is written down in one place, and so the metadata arrives as an object a script
// can index by key rather than as the proto's list of pairs.
//
// The transcript is not in here. It is far past what an environment variable holds, and a
// script that wants it has the agent's id and the address of the service.
type actionPayload struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	State     string            `json:"state"`
	Alive     bool              `json:"alive"`
	Cwd       string            `json:"cwd"`
	Harness   string            `json:"harness"`
	SessionID string            `json:"session_id"`
	Flags     string            `json:"flags"`
	Prompt    string            `json:"prompt"`
	Pane      string            `json:"pane"`
	Workflow  string            `json:"workflow"`
	Mark      string            `json:"mark"`
	Hidden    bool              `json:"hidden"`
	CreatedAt string            `json:"created_at"`
	Metadata  map[string]string `json:"metadata"`
}

// actionEnv is the environment an action runs with, over the panel's own.
//
// The agent goes in whole as JSON under one name. The two values beside it are what a
// script reaches back through: the address of the service, which is where everything about
// the agent that is too large to pass in an environment variable is, and the socket of the
// manager's own tmux server. Without that socket a script sending keys to the agent's pane
// would be addressing the user's server, where that pane id means something else or
// nothing at all.
func (m model) actionEnv() ([]string, error) {
	agent := m.actions.agent

	payload := actionPayload{
		ID:        agent.ID,
		Name:      agent.Name,
		State:     agent.State,
		Alive:     m.actions.alive,
		Cwd:       agent.Cwd,
		Harness:   agent.Harness,
		SessionID: agent.SessionID,
		Flags:     agent.Flags,
		Prompt:    agent.Prompt,
		Pane:      agent.Pane,
		Workflow:  m.workflowName(agent.WorkflowID),
		Hidden:    agent.Hidden,
		Metadata:  make(map[string]string, len(agent.Metadata)),
	}
	if agent.Mark != nil {
		payload.Mark = *agent.Mark
	}
	if !agent.CreatedAt.IsZero() {
		payload.CreatedAt = agent.CreatedAt.Format(time.RFC3339)
	}
	for _, entry := range agent.Metadata {
		payload.Metadata[entry.Key] = entry.Value
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the agent: %w", err)
	}

	return []string{
		"WEAVE_AGENT=" + string(encoded),
		"WEAVE_AGENT_ID=" + agent.ID,
		"WEAVE_ADDR=" + m.addr,
		"WEAVE_TMUX_SOCKET=" + m.tmuxSocket,
	}, nil
}

// workflowName is the name of the workflow an agent belongs to, or empty for an id the
// panel has not listed. It reads the listing the panel already polls, the way the schedule
// name does.
func (m model) workflowName(id string) string {
	for _, workflow := range m.tree.workflows {
		if workflow.ID == id {
			return workflow.Name
		}
	}

	return ""
}

// noActionsMarkdown is what the stage shows with nothing to list, so the pane says where the
// panel looked and what it wanted to find rather than sitting blank.
func noActionsMarkdown(dir string) string {
	return fmt.Sprintf("# Actions\n\n_nothing here yet_\n\nAn action is any executable file in\n\n"+
		"    %s\n\nIt runs on the agent you opened this list from, in that agent's directory, with "+
		"the agent as JSON in `WEAVE_AGENT`. Where its output goes is up to it.\n", dir)
}

// maxActionPreview bounds how much of an action the stage reads. The renderer works over the
// whole document it is handed, and an action can be a compiled program of any size, so the
// preview is a head of the file rather than all of it.
const maxActionPreview = 64 * 1024

// stageAction shows the file under the cursor in the stage. The file is all the panel has to
// say about an action, since nothing is read out of it, so what one does is only ever legible
// from the action itself.
//
// It goes through the same rendered document every other preview does, so a script is drawn in
// the panel's own palette and sits in the margins the rest of the stage uses.
func (m model) stageAction() {
	action, ok := m.selectedAction()
	if !ok {
		m.stage.Text(noActionsMarkdown(m.actionsDir))

		return
	}

	contents, truncated, err := readHead(action.Path, maxActionPreview)
	if err != nil {
		m.stage.Text(fmt.Sprintf("# %s\n\n_%s_\n", action.Name, errui.Reason(err)))

		return
	}

	m.stage.Text(actionMarkdown(action, contents, truncated))
}

// readHead reads up to limit bytes of a file and reports whether there was more. A truncated
// read is cut back to its last complete line, which keeps a document from ending mid-character
// and reading as a file that is not text at all.
func readHead(path string, limit int) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, limit+1)
	read, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, false, fmt.Errorf("failed to read %s: %w", path, err)
	}

	if read <= limit {
		return head[:read], false, nil
	}

	head = head[:limit]
	if lastLine := bytes.LastIndexByte(head, '\n'); lastLine > 0 {
		head = head[:lastLine]
	}

	return head, true, nil
}

// actionsView renders the actions list, naming the agent they would run on. The list acts on
// the row it was opened from rather than on whatever the agents list is hovering by the time a
// script is chosen, so the name says which agent it has. A file without the executable bit is
// drawn dim, the way a dormant agent is, since it is there and it will not run.
func (m model) actionsView() string {
	header := headerStyle.Render("Actions on "+m.actions.agent.Name) + "\n\n"
	hints := "\n" + m.actionHints()

	if len(m.actions.items) == 0 {
		return header + dimStyle.Render("no actions") + "\n" + hints
	}

	rows := make([]string, 0, len(m.actions.items))
	for i, action := range m.actions.items {
		prefix := "  "
		name := action.Name
		if !action.Executable {
			name = dimStyle.Render(name)
		}
		if i == m.actions.cursor {
			prefix = cursorStyle.Render("› ")
			name = cursorStyle.Render(action.Name)
		}

		rows = append(rows, prefix+name)
	}

	return header + renderList(rows, m.actions.cursor, m.listLines(header, hints)) + hints
}
