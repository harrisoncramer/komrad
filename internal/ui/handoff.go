package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

var errNoLiveWriter = errors.New("this task has no live writer, so start a new agent on it instead")

// handOffEntry is the entry that hands a task from its live writer to a new one.
func handOffEntry(m *model, task panel.Task) submenuEntry {
	writer, ok := m.listing.findLiveWriter(task.ID)
	entry := openEntry("hand off", m.keys.Menu.HandOff, func(m *model) tea.Cmd {
		if !ok {
			m.report("Could not hand off task "+task.Slug, errNoLiveWriter)

			return nil
		}

		return openHandOff(m, task, writer)
	})
	entry.isOff = !ok

	return entry
}

type handOffValues struct {
	Note    string
	Harness string
	Flags   string
}

// openHandOff opens the form that hands a task from its writer to a new one.
func openHandOff(m *model, task panel.Task, writer panel.Agent) tea.Cmd {
	values := &handOffValues{Harness: writer.Harness, Flags: writer.Flags}

	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind: formAgent,
		Build: func(m *model) (*huh.Form, []string) {
			return handOffForm(fixedKeys(), values, task, writer)
		},
		Editable: []editableText{{key: "note", value: &values.Note}},
		Submit:   holdSubmit(func(m *model) tea.Cmd { return m.handOffTask(task, writer, *values) }),
	}))
}

// handOffTask archives the task's writer with its children, then starts a new writer in the same
// checkout.
func (m *model) handOffTask(task panel.Task, writer panel.Agent, values handOffValues) tea.Cmd {
	children := xslice.Filter(m.listing.liveAgents(), func(agent panel.Agent) bool {
		return agent.ParentAgentID != nil && *agent.ParentAgentID == writer.ID
	})
	pending := m.unstageAhead(slices.Concat([]panel.Agent{writer}, children))

	return pending.attach(m.agentWriteCmd(
		"handing off "+task.Slug,
		fmt.Sprintf("Could not hand off task %q", task.Slug),
		func(a api) (*agentv1.Agent, error) {
			if err := pending.noteArchive(a.archiveAgents([]string{writer.ID})); err != nil {
				return nil, err
			}

			created, err := a.createAgent(createAgentParams{
				Prompt:     handOffPrompt(writer.Name, values.Note),
				Harness:    values.Harness,
				Flags:      values.Flags,
				WorkflowID: task.WorkflowID,
				Task:       task.ID,
			})
			if err != nil {
				return nil, errors.Join(err, a.restoreAgents([]string{writer.ID}))
			}

			return created, nil
		},
	))
}

// handOffPrompt is the new writer's brief, naming the writer it takes over from.
func handOffPrompt(previous, note string) string {
	prompt := fmt.Sprintf("You take this task over from %s, who wrote it until now. Read the task's notes "+
		"and run git status before you change anything.", previous)
	if note = strings.TrimSpace(note); note != "" {
		prompt += "\n\n" + note
	}

	return prompt
}

func handOffForm(keys config.FixedKeys, values *handOffValues, task panel.Task, writer panel.Agent) (*huh.Form, []string) {
	return formOf(keys,
		textField("note").Title(fmt.Sprintf("Hand %q off from %q", task.Slug, writer.Name)).
			Description(fmt.Sprintf("what the new writer is told; %s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).
			Value(&values.Note),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	)
}
