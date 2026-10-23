package gitmenu

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

type report func(screen.Inputs) screen.Request

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
func (g Menus) planOperation(op operation) screen.Request {
	clients := g.deps.Clients

	return screen.Write{
		Doing: op.planning,
		Send: func() screen.WriteResult {
			confirm, err := op.plan(clients)

			return screen.WriteResult{
				FollowUpReplacesForm: err == nil,
				FollowUp:             plannedMsg{op: op, form: confirm, err: err},
			}
		},
	}
}

// operationWrite is the write that runs the operation and reports what it did.
func (g Menus) operationWrite(op operation) screen.Write {
	clients := g.deps.Clients

	return screen.Write{
		Doing: op.running,
		Send: func() screen.WriteResult {
			return screen.WriteResult{FollowUp: doneMsg{report: op.run(clients)}}
		},
	}
}

// applyPlanned opens the confirmation, or says why there is nothing to confirm.
func (g Menus) applyPlanned(msg plannedMsg) screen.Request {
	if msg.err == nil {
		return g.openConfirm(msg.op, msg.form)
	}

	var skipped skippedError
	if errors.As(msg.err, &skipped) {
		return screen.NewPostMessage(screen.Warning, string(skipped))
	}

	return screen.Report{Summary: msg.op.planFailed, Err: msg.err}
}

// applyDone reports the outcome.
func applyDone(in screen.Inputs, msg doneMsg) screen.Request {
	return msg.report(in)
}

func (g Menus) openConfirm(op operation, buildForm confirmForm) screen.Request {
	built, confirmed := buildForm(config.Fixed())

	return form.Open{Form: form.Form{
		Prebuilt: built,
		Build: func(form.Inputs) (*huh.Form, []string) {
			held, _ := buildForm(config.Fixed())

			return held, nil
		},
		Submit: func(form.Inputs) screen.Request {
			if !*confirmed {
				return nil
			}

			return g.operationWrite(op)
		},
		Narrow:          true,
		ClosesOnRefusal: true,
	}}
}
