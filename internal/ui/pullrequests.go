package ui

import (
	"context"
	"strings"

	"komrad/internal/ui/api"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

// pullRequestFields are the record's rows for the branch's pull requests: the current one and its
// author, then the rest, all of them when the branch has moved past its last one.
func pullRequestFields(standing panel.PullRequestStanding, pullRequests []panel.PullRequest) []record.Field {
	if len(pullRequests) == 0 {
		return nil
	}

	var fields []record.Field
	rest := pullRequests
	if standing.State != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE {
		current := pullRequests[0]
		rest = pullRequests[1:]
		if current.Title != "" {
			fields = append(fields, record.Field{Label: current.Noun(), Value: current.Ref() + " " + current.Title})
		}
		if current.Author != "" {
			fields = append(fields, record.Field{Label: "author", Value: "@" + current.Author})
		}
	}
	if len(rest) == 0 {
		return fields
	}

	words := xslice.Map(rest, func(pullRequest panel.PullRequest) string {
		return pullRequest.Ref() + " " + pullRequest.Words()
	})

	return append(fields, record.Field{Label: rest[0].Noun() + "s", Value: strings.Join(words, ", ")})
}

// openPullRequest hands the agent's current pull request to the browser, or says why there is
// none to open.
func (m *model) openPullRequest(task panel.Task) screen.Request {
	standing := task.PullRequest
	if standing.URL == "" {
		return screen.Report{Summary: "Could not open the pull request of " + task.Slug, Err: pullRequestRefusal(task)}
	}

	return screen.Cmd(m.openURLCmd(standing.URL))
}

// copyPullRequestURL puts the agent's current pull request's address on the clipboard, or says
// why there is none to copy.
func (m *model) copyPullRequestURL(task panel.Task) screen.Request {
	standing := task.PullRequest
	if standing.URL == "" {
		return screen.Report{Summary: "Could not copy the pull request of " + task.Slug, Err: pullRequestRefusal(task)}
	}

	return screen.Copy{Text: standing.URL}
}

// urlOpenedMsg reports that the panel asked the browser to open an address.
type urlOpenedMsg struct {
	target string
	err    error
}

// openURLCmd hands an address to the browser off the update.
func (m *model) openURLCmd(target string) tea.Cmd {
	opener := m.browser

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), api.RequestTimeout)
		defer cancel()

		return urlOpenedMsg{target: target, err: opener.OpenURL(ctx, target)}
	}
}

func (m *model) applyURLOpened(msg urlOpenedMsg) screen.Request {
	return screen.Report{Summary: "Could not open " + msg.target, Err: msg.err}
}
