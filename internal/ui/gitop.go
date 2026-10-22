package ui

import (
	"errors"

	"komrad/config"
	"komrad/internal/ui/form"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

type gitOp struct {
	planning   string
	planFailed string
	plan       func(api) (gitConfirmForm, error)
	running    string
	run        func(api) gitReport
}

// gitSkippedError is why an operation has nothing to do, said as a warning rather than a failure.
type gitSkippedError string

func (s gitSkippedError) Error() string {
	return string(s)
}

type gitConfirmForm func(config.FixedKeys) (*huh.Form, *bool)

type gitReport func(*model) tea.Cmd

// gitPlannedMsg is a plan come back, or the reason there is none.
type gitPlannedMsg struct {
	op   gitOp
	form gitConfirmForm
	err  error
}

// gitDoneMsg is a finished git operation.
type gitDoneMsg struct {
	report gitReport
}

// planGitCmd asks what the operation would do.
func (m *model) planGitCmd(op gitOp) tea.Cmd {
	clients := m.api

	return m.apply(screen.Write{
		Doing: op.planning,
		Send: func() screen.Landed {
			form, err := op.plan(clients)

			return screen.Landed{
				Replaces: err == nil,
				Then:     gitPlannedMsg{op: op, form: form, err: err},
			}
		},
	})
}

// gitOpWrite is the write that runs the operation and reports what it did.
func (m *model) gitOpWrite(op gitOp) screen.Write {
	clients := m.api

	return screen.Write{
		Doing: op.running,
		Send: func() screen.Landed {
			return screen.Landed{Then: gitDoneMsg{report: op.run(clients)}}
		},
	}
}

// applyGitPlanned opens the confirmation, or says why there is nothing to confirm.
func (m *model) applyGitPlanned(msg gitPlannedMsg) tea.Cmd {
	if msg.err == nil {
		return m.openGitConfirm(msg.op, msg.form)
	}

	var skipped gitSkippedError
	if errors.As(msg.err, &skipped) {
		m.note(messageWarning, string(skipped))
	} else {
		m.report(msg.op.planFailed, msg.err)
	}

	return nil
}

// applyGitDone reports the outcome.
func (m *model) applyGitDone(msg gitDoneMsg) tea.Cmd {
	return msg.report(m)
}

func (m *model) openGitConfirm(op gitOp, buildForm gitConfirmForm) tea.Cmd {
	built, confirmed := buildForm(fixedKeys())

	return m.apply(form.Open{Form: form.Form{
		Built: built,
		Build: func(form.Inputs) (*huh.Form, []string) {
			held, _ := buildForm(fixedKeys())

			return held, nil
		},
		Submit: func(form.Inputs) screen.Request {
			if !*confirmed {
				return nil
			}

			return m.gitOpWrite(op)
		},
		Narrow:          true,
		ClosesOnRefusal: true,
	}})
}
