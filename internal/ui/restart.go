package ui

import (
	"errors"
	"fmt"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

var (
	errNoWriter           = errors.New("this task has no writer, so start a new agent on it instead")
	errWriterNeverStarted = errors.New("this task's writer never started, so retry it instead")
)

type restartValues struct {
	Note    string
	Harness string
	Flags   string
}

// restartTarget is the agent a restart form relaunches, what it works on, and what it is briefed from.
type restartTarget struct {
	agent    panel.Agent
	on       string
	briefing string
}

// restartEntries is the entry that restarts a task's live writer on a new conversation, and none
// when no agent can start on the task.
func restartEntries(m *model, task panel.Task) []submenuEntry {
	if !isTaskStartable(m, task) {
		return nil
	}

	writer, refusal := m.listing.findRestartableWriter(task)
	entry := openEntry("restart", m.keys.Menu.Restart, func(m *model) tea.Cmd {
		if refusal != nil {
			m.report("Could not restart the writer of task "+task.Slug, refusal)

			return nil
		}

		return openRestart(m, restartTarget{agent: *writer, on: task.Slug, briefing: "the task's notes"})
	})
	entry.isOff = refusal != nil

	return []submenuEntry{entry}
}

// findRestartableWriter is the task's writer when it can restart, or why it cannot.
func (l listing) findRestartableWriter(task panel.Task) (*panel.Agent, error) {
	writer, ok := l.findTaskWriter(task)
	if !ok {
		return nil, errNoWriter
	}
	if !isLiveWriter(writer.Agent) {
		return nil, errWriterNeverStarted
	}

	return &writer.Agent, nil
}

// orchestratorRestartEntries is the entry that restarts the workflow's orchestrator on a new
// conversation, and none when the workflow has no orchestrator.
func orchestratorRestartEntries(m *model, workflow panel.Workflow, orchestrator *panel.Agent) []submenuEntry {
	if orchestrator == nil {
		return nil
	}

	return []submenuEntry{openEntry("restart orchestrator", m.keys.Menu.Restart, func(m *model) tea.Cmd {
		return openRestart(m, restartTarget{agent: *orchestrator, on: workflow.Name, briefing: "its briefing"})
	})}
}

// openRestart opens the form that restarts an agent on a new conversation.
func openRestart(m *model, target restartTarget) tea.Cmd {
	values := &restartValues{
		Harness: target.agent.Harness,
		Flags:   target.agent.Flags,
	}

	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind: formAgent,
		Build: func(m *model) (*huh.Form, []string) {
			return restartForm(fixedKeys(), values, target)
		},
		Editable: []editableText{{key: "note", value: &values.Note}},
		Submit:   holdSubmit(func(m *model) tea.Cmd { return m.restartAgent(target.agent, *values) }),
	}))
}

// restartAgent relaunches the agent on a new conversation in the same directory.
func (m *model) restartAgent(agent panel.Agent, values restartValues) tea.Cmd {
	pending := m.unstageAhead([]panel.Agent{agent})

	return pending.attach(m.agentWriteCmd(
		"restarting "+m.listing.agentName(agent),
		fmt.Sprintf("Could not restart agent %q", m.listing.agentName(agent)),
		func(a api) (*agentv1.Agent, error) {
			return a.restartAgent(restartAgentParams{
				ID:      agent.ID,
				Note:    values.Note,
				Harness: values.Harness,
				Flags:   values.Flags,
			})
		},
	))
}

func restartForm(fixed config.FixedKeys, values *restartValues, target restartTarget) (*huh.Form, []string) {
	return formOf(fixed,
		textField("note").Title(fmt.Sprintf("Restart %q on %q with a new conversation", target.agent.Name, target.on)).
			Description(fmt.Sprintf("what the agent is told on top of %s; %s opens $EDITOR; %s submits", target.briefing, keys.Label(fixed.Editor), keys.Label(fixed.Submit))).
			Value(&values.Note),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	)
}
