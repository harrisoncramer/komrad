package ui

import (
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// abortedMsg is a finished abort.
type abortedMsg struct {
	name     string
	sequence panel.Sequence
	branch   string
	// parkedRef holds the worktree as it stood before the abort.
	parkedRef   string
	interrupted bool
	err         error
	write       int
}

// abortCmd runs the abort and reports what it did.
func (m *model) abortCmd(agentID, name string, sequence panel.Sequence) tea.Cmd {
	clients := m.api
	write := m.beginWrite("aborting the " + string(sequence) + " of " + name)

	return func() tea.Msg {
		result, err := clients.abortSequence(agentID)
		if err != nil {
			return abortedMsg{
				name:     name,
				sequence: sequence,
				err:      err,
				write:    write,
			}
		}

		return abortedMsg{
			write:       write,
			name:        name,
			sequence:    sequenceFrom(result.GetSequence()),
			branch:      result.GetBranch(),
			parkedRef:   result.GetParkedRef(),
			interrupted: result.GetInterrupted(),
		}
	}
}

// applyAborted reports the outcome, and takes down the confirmation held for it.
func (m *model) applyAborted(msg abortedMsg) (tea.Model, tea.Cmd) {
	held := m.landHeld(msg.write)

	m.reportAborted(msg)

	if held {
		return m.closeForm()
	}

	return m, nil
}

// openAbortConfirm asks before undoing what an agent's worktree has part-way through.
func (m model) openAbortConfirm(agentID, name string, sequence panel.Sequence) (model, tea.Cmd) {
	form, confirmed := abortForm(name, sequence, formKeys())

	return m, m.openForm(formState{
		kind:   formAgentAbort,
		form:   form,
		update: model.updateForm,
		submit: holdSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			cmd := m.abortCmd(agentID, name, sequence)

			return m, cmd
		}),
		rebuild: func(model) (*huh.Form, []string) {
			held, _ := abortForm(name, sequence, formKeys())

			return held, nil
		},
	})
}

func (m *model) reportAborted(msg abortedMsg) {
	if msg.err != nil {
		m.report(fmt.Sprintf("Could not abort the %s of %s", msg.sequence, msg.name), msg.err)

		return
	}

	where := "The checkout"
	if msg.branch != "" {
		where = msg.branch
	}

	lines := []string{where + " is back where it stood before.", "A copy of the worktree from before the abort is kept at " + msg.parkedRef + "."}
	if msg.interrupted {
		lines = append(lines, "Its turn was cut first.")
	}

	m.pushMessage(message{
		kind:    messageInfo,
		summary: fmt.Sprintf("%s: aborted the %s", msg.name, msg.sequence),
		detail:  strings.Join(lines, " "),
	})
}

func abortForm(name string, sequence panel.Sequence, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Abort the %s in the worktree of %s?", sequence, name),
		fmt.Sprintf("The branch goes back to where it stood before the %s began, and its files with it. "+
			"A copy of the worktree as it stands now, conflicts and all, is kept first. "+
			"%s is stopped first if it is working, and is told either way.", sequence, name),
		"Abort "+string(sequence),
		keys,
	)
}
