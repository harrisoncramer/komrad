package ui

import (
	"komrad/config"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

type gitOp struct {
	kind       formKind
	planning   string
	planFailed string
	plan       func(api) (gitConfirmForm, error)
	running    string
	run        func(api) gitReport
}

type gitConfirmForm func(config.FormKeys) (*huh.Form, *bool)

type gitReport func(*model) tea.Cmd

// gitPlannedMsg is a plan come back, or the reason there is none.
type gitPlannedMsg struct {
	op    gitOp
	form  gitConfirmForm
	err   error
	write int
}

// gitDoneMsg is a finished git operation.
type gitDoneMsg struct {
	report gitReport
	write  int
}

// planGitCmd asks what the operation would do.
func (m *model) planGitCmd(op gitOp) tea.Cmd {
	clients := m.api
	write := m.beginWrite(op.planning)

	return func() tea.Msg {
		form, err := op.plan(clients)

		return gitPlannedMsg{
			op:    op,
			form:  form,
			err:   err,
			write: write,
		}
	}
}

// runGitCmd runs the operation and reports what it did.
func (m *model) runGitCmd(op gitOp) tea.Cmd {
	clients := m.api
	write := m.beginWrite(op.running)

	return func() tea.Msg {
		return gitDoneMsg{report: op.run(clients), write: write}
	}
}

// applyGitPlanned opens the confirmation, or says why there is nothing to confirm.
func (m model) applyGitPlanned(msg gitPlannedMsg) (tea.Model, tea.Cmd) {
	held := m.landHeld(msg.write)

	if msg.err != nil {
		m.report(msg.op.planFailed, msg.err)

		if held {
			return m.closeForm()
		}

		return m, nil
	}

	return m.openGitConfirm(msg.op, msg.form)
}

// applyGitDone reports the outcome, and takes down the confirmation held for it.
func (m model) applyGitDone(msg gitDoneMsg) (tea.Model, tea.Cmd) {
	held := m.landHeld(msg.write)

	reported := msg.report(&m)

	if !held {
		return m, reported
	}

	next, back := m.closeForm()

	return next, tea.Batch(reported, back)
}

func (m model) openGitConfirm(op gitOp, confirmForm gitConfirmForm) (tea.Model, tea.Cmd) {
	form, confirmed := confirmForm(formKeys())

	return m, m.openForm(formState{
		kind:   op.kind,
		form:   form,
		update: model.updateForm,
		submit: holdSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			cmd := m.runGitCmd(op)

			return m, cmd
		}),
		rebuild: func(model) (*huh.Form, []string) {
			held, _ := confirmForm(formKeys())

			return held, nil
		},
	})
}
