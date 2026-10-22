package ui

import (
	"errors"
	"fmt"

	"komrad/config"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

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
func restartEntries(m *model, task panel.Task) []menu.Entry {
	if !isTaskStartable(m, task) {
		return nil
	}

	writer, refusal := findRestartableWriter(m.listing, task)
	entry := menu.Leaving("restart", m.keys.Menu.Restart, func() screen.Request {
		if refusal != nil {
			return screen.Report{Summary: "Could not restart the writer of task " + task.Slug, Err: refusal}
		}

		return openRestart(m, restartTarget{agent: *writer, on: task.Slug, briefing: "the task's notes"})
	})
	entry.Off = refusal != nil

	return []menu.Entry{entry}
}

// findRestartableWriter is the task's writer when it can restart, or why it cannot.
func findRestartableWriter(l panel.Listing, task panel.Task) (*panel.Agent, error) {
	writer, ok := l.FindTaskWriter(task)
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
func orchestratorRestartEntries(m *model, workflow panel.Workflow, orchestrator *panel.Agent) []menu.Entry {
	if orchestrator == nil {
		return nil
	}

	return []menu.Entry{menu.Leaving("restart orchestrator", m.keys.Menu.Restart, func() screen.Request {
		return openRestart(m, restartTarget{agent: *orchestrator, on: workflow.Name, briefing: "its briefing"})
	})}
}

// openRestart opens the form that restarts an agent on a new conversation.
func openRestart(m *model, target restartTarget) screen.Request {
	values := &restartValues{
		Harness: target.agent.Harness,
		Flags:   target.agent.Flags,
	}

	return form.Open{Form: form.Form{
		Build: func(form.Inputs) (*huh.Form, []string) {
			return restartForm(fixedKeys(), values, target)
		},
		Editable: []form.Editable{{Key: "note", Value: &values.Note}},
		Submit:   func(form.Inputs) screen.Request { return m.restartWrite(target.agent, *values) },
	}}
}

// restartWrite is the write that relaunches the agent on a new conversation in the same directory.
func (m *model) restartWrite(agent panel.Agent, values restartValues) screen.Write {
	clients := m.api

	return screen.Write{
		Doing:   "restarting " + m.listing.AgentName(agent),
		Summary: fmt.Sprintf("Could not restart agent %q", m.listing.AgentName(agent)),
		Unstage: []panel.Agent{agent},
		Send: func() screen.Landed {
			restarted, err := clients.restartAgent(restartAgentParams{
				ID:      agent.ID,
				Note:    values.Note,
				Harness: values.Harness,
				Flags:   values.Flags,
			})
			if err != nil {
				return screen.Landed{Err: err}
			}

			return screen.Landed{Made: madeAgent(restarted, false)}
		},
	}
}

func restartForm(fixed config.FixedKeys, values *restartValues, target restartTarget) (*huh.Form, []string) {
	return form.Of(fixed,
		form.Text("note").Title(fmt.Sprintf("Restart %q on %q with a new conversation", target.agent.Name, target.on)).
			Description(fmt.Sprintf("what the agent is told on top of %s; %s opens $EDITOR; %s submits", target.briefing, keys.Label(fixed.Editor), keys.Label(fixed.Submit))).
			Value(&values.Note),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	)
}
