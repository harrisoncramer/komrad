package ui

import (
	"errors"
	"fmt"

	"komrad/config"
	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

var errNoLiveWriter = errors.New("this task has no live writer, so start a new agent on it instead")

type restartValues struct {
	Note string
}

// restartEntries is the entry that restarts a task's live writer on a new conversation, and none
// when no agent can start on the task.
func restartEntries(m *model, task panel.Task) []submenuEntry {
	if !isTaskStartable(m, task) {
		return nil
	}

	writer, ok := m.listing.findLiveWriter(task.ID)
	entry := openEntry("restart", m.keys.Menu.Restart, func(m *model) tea.Cmd {
		if !ok {
			m.report("Could not restart the writer of task "+task.Slug, errNoLiveWriter)

			return nil
		}

		return openRestart(m, task, writer)
	})
	entry.isOff = !ok

	return []submenuEntry{entry}
}

// openRestart opens the form that restarts a task's writer on a new conversation.
func openRestart(m *model, task panel.Task, writer panel.Agent) tea.Cmd {
	values := &restartValues{}

	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind: formAgent,
		Build: func(m *model) (*huh.Form, []string) {
			return restartForm(fixedKeys(), values, task, writer)
		},
		Editable: []editableText{{key: "note", value: &values.Note}},
		Submit:   holdSubmit(func(m *model) tea.Cmd { return m.restartWriter(writer, *values) }),
	}))
}

// restartWriter relaunches the writer on a new conversation in the same checkout, taking its pane
// off the stage first since the restart kills it.
func (m *model) restartWriter(writer panel.Agent, values restartValues) tea.Cmd {
	pending := m.unstageAhead([]panel.Agent{writer})

	return pending.attach(m.agentWriteCmd(
		"restarting "+writer.Name,
		fmt.Sprintf("Could not restart agent %q", writer.Name),
		func(a api) (*agentv1.Agent, error) {
			return a.restartAgent(writer.ID, values.Note)
		},
	))
}

func restartForm(keys config.FixedKeys, values *restartValues, task panel.Task, writer panel.Agent) (*huh.Form, []string) {
	return formOf(keys,
		textField("note").Title(fmt.Sprintf("Restart %q on %q with a new conversation", writer.Name, task.Slug)).
			Description(fmt.Sprintf("what the writer is told on top of the task's notes; %s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).
			Value(&values.Note),
	)
}
