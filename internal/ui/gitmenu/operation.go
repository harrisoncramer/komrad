package ui

import (
	"errors"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/screen"

	"charm.land/huh/v2"
)

type operation struct {
	planning   string
	planFailed string
	plan       func(api.Clients) (confirmForm, error)
	running    string
	run        func(api.Clients) report
}

// skippedError is why an operation has nothing to do, said as a warning rather than a failure.
type skippedError string

func (s skippedError) Error() string {
	return string(s)
}

type confirmForm func(config.FixedKeys) (*huh.Form, *bool)

type report func(*model) screen.Request

// plannedMsg is a plan come back, or the reason there is none.
type plannedMsg struct {
	op   operation
	form confirmForm
	err  error
}

// doneMsg is a finished git operation.
type doneMsg struct {
	report report
}

// planOperation asks what the operation would do.
func (m *model) planOperation(op operation) screen.Request {
	clients := m.api

	return screen.Write{
		Doing: op.planning,
		Send: func() screen.Landed {
			confirm, err := op.plan(clients)

			return screen.Landed{
				Replaces: err == nil,
				Then:     plannedMsg{op: op, form: confirm, err: err},
			}
		},
	}
}

// operationWrite is the write that runs the operation and reports what it did.
func (m *model) operationWrite(op operation) screen.Write {
	clients := m.api

	return screen.Write{
		Doing: op.running,
		Send: func() screen.Landed {
			return screen.Landed{Then: doneMsg{report: op.run(clients)}}
		},
	}
}

// applyPlanned opens the confirmation, or says why there is nothing to confirm.
func (m *model) applyPlanned(msg plannedMsg) screen.Request {
	if msg.err == nil {
		return m.openConfirm(msg.op, msg.form)
	}

	var skipped skippedError
	if errors.As(msg.err, &skipped) {
		return screen.NewNote(screen.Warning, string(skipped))
	}

	return screen.Report{Summary: msg.op.planFailed, Err: msg.err}
}

// applyDone reports the outcome.
func (m *model) applyDone(msg doneMsg) screen.Request {
	return msg.report(m)
}

func (m *model) openConfirm(op operation, buildForm confirmForm) screen.Request {
	built, confirmed := buildForm(fixedKeys())

	return form.Open{Form: form.Form{
		Built: built,
		Build: func(form.Inputs) (*huh.Form, []string) {
			held, _ := buildForm(fixedKeys())

			return held, nil
		},
		Submit: func(form.Inputs) screen.Request {
			if !*confirmed {
				return nil
			}

			return m.operationWrite(op)
		},
		Narrow:          true,
		ClosesOnRefusal: true,
	}}
}
