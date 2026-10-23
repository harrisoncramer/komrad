package gitmenu

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	forgev1 "komrad/proto/gen/forge/v1"

	"charm.land/bubbles/v2/textarea"
	"charm.land/huh/v2"
)

var errNoTitle = errors.New("a pull request needs a title")

// textReadMsg is a task's pull request's title and description as the forge holds them, or the
// reason the forge would not give them.
type textReadMsg struct {
	task panel.Task
	text *forgev1.PullRequestText
	err  error
}

// readText asks the forge for the title and description of the task's pull request, or says why
// there is none to edit.
func (g Menus) readText(task panel.Task) screen.Request {
	standing := task.PullRequest
	if standing.URL == "" {
		return screen.Report{Summary: "Could not edit the pull request of " + task.Slug, Err: pullRequestRefusal(task)}
	}

	clients := g.deps.Clients

	return screen.Write{
		InFlightText: "reading " + standing.Ref(),
		Send: func() screen.WriteResult {
			text, err := clients.ReadPullRequestText(standing.URL)

			return screen.WriteResult{
				FollowUpReplacesForm: err == nil,
				FollowUp:             textReadMsg{task: task, text: text, err: err},
			}
		},
	}
}

// applyTextRead opens the edit form, or says why there is none.
func (g Menus) applyTextRead(msg textReadMsg) screen.Request {
	if msg.err != nil {
		return screen.Report{Summary: "Could not edit " + msg.task.PullRequest.Ref() + " of " + msg.task.Slug, Err: msg.err}
	}

	return g.openEditForm(msg.task, msg.text)
}

// editFormValues is the edit form's answers.
type editFormValues struct {
	Title string
	Body  string
}

// openEditForm opens the form that changes the pull request's title and description, starting
// from the text the forge gave.
func (g Menus) openEditForm(task panel.Task, read *forgev1.PullRequestText) screen.Request {
	started := editFormValues{Title: read.GetTitle(), Body: fieldBody(read.GetBody())}
	values := xptr.Ptr(started)

	return form.Open{Form: form.Form{
		Build: func(form.Inputs) (*huh.Form, []string) {
			return editForm(config.Fixed(), "Edit "+task.PullRequest.Ref(), values)
		},
		Editable: []form.Editable{{Key: "body", Value: &values.Body}},
		Submit: func(form.Inputs) screen.Request {
			if strings.TrimSpace(values.Title) == "" {
				return form.Refuse{Err: errNoTitle, ReturnToField: "title"}
			}
			if *values == started {
				return nil
			}

			return g.editWrite(task, values.editRequest(task.PullRequest.URL, read, started))
		},
		CanUseNarrowestWidth: true,
		ClosesOnRefusal:      true,
	}}
}

// fieldBody is the description as the form's text field holds it, one newline to each line break.
func fieldBody(body string) string {
	field := textarea.New()
	field.SetValue(strings.ReplaceAll(body, "\r\n", "\n"))

	return field.Value()
}

// editRequest is the edit the form asks for, carrying only the fields the person changed.
func (v editFormValues) editRequest(address string, read *forgev1.PullRequestText, started editFormValues) *forgev1.EditForgePullRequestRequest {
	req := &forgev1.EditForgePullRequestRequest{Url: address, EditedFrom: read}
	if v.Title != started.Title {
		req.Title = &v.Title
	}
	if v.Body != started.Body {
		req.Body = &v.Body
	}

	return req
}

// editWrite is the write that changes the pull request's title, description or both.
func (g Menus) editWrite(task panel.Task, req *forgev1.EditForgePullRequestRequest) screen.Write {
	clients := g.deps.Clients
	ref := task.PullRequest.Ref()

	return screen.Write{
		InFlightText:   "editing " + ref,
		FailureSummary: "Could not edit " + ref + " of " + task.Slug,
		Send: func() screen.WriteResult {
			return screen.WriteResult{
				Err:           clients.EditPullRequest(req),
				WaitFor:       screen.WaitFor{Shows: showsTask(task.ID, func(task panel.Task) bool { return showsEdit(task, req) })},
				SuccessNotice: &screen.Notice{Summary: task.Slug + ": edited " + ref, Detail: cmp.Or(req.GetTitle(), task.PullRequest.Title)},
			}
		},
	}
}

// showsEdit reports whether the task's current pull request holds the text the edit wrote.
func showsEdit(task panel.Task, req *forgev1.EditForgePullRequestRequest) bool {
	if req.Title != nil && task.PullRequest.Title != req.GetTitle() {
		return false
	}
	if req.Body == nil {
		return true
	}

	for _, pullRequest := range task.PullRequests {
		if pullRequest.URL == task.PullRequest.URL {
			return pullRequest.Body == req.GetBody()
		}
	}

	return true
}

// editForm asks for a pull request's title and description.
func editForm(fixed config.FixedKeys, title string, values *editFormValues) (*huh.Form, []string) {
	return form.Of(fixed,
		huh.NewInput().Key("title").Title(title).Description("the title").Value(&values.Title),
		form.Text("body").Title("Description").
			Description(fmt.Sprintf("%s opens $EDITOR; %s saves", keys.Label(fixed.Editor), keys.Label(fixed.Submit))).
			Value(&values.Body),
	)
}
