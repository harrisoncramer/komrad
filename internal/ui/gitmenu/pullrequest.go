package gitmenu

import (
	"context"

	"komrad/internal/ui/api"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// urlOpenedMsg reports that the panel asked the browser to open an address.
type urlOpenedMsg struct {
	target string
	err    error
}

// openPullRequest hands the agent's current pull request to the browser, or says why there is
// none to open.
func (g Menus) openPullRequest(task panel.Task) screen.Request {
	standing := task.PullRequest
	if standing.URL == "" {
		return screen.Report{Summary: "Could not open the pull request of " + task.Slug, Err: pullRequestRefusal(task)}
	}

	return screen.RunCmd(g.openURLCmd(standing.URL))
}

// copyPullRequestURL puts the agent's current pull request's address on the clipboard, or says
// why there is none to copy.
func copyPullRequestURL(task panel.Task) screen.Request {
	standing := task.PullRequest
	if standing.URL == "" {
		return screen.Report{Summary: "Could not copy the pull request of " + task.Slug, Err: pullRequestRefusal(task)}
	}

	return screen.Copy{Text: standing.URL}
}

// openURLCmd hands an address to the browser off the update.
func (g Menus) openURLCmd(target string) tea.Cmd {
	opener := g.deps.Browser

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), api.RequestTimeout)
		defer cancel()

		return urlOpenedMsg{target: target, err: opener.OpenURL(ctx, target)}
	}
}

func applyURLOpened(msg urlOpenedMsg) screen.Request {
	return screen.Report{Summary: "Could not open " + msg.target, Err: msg.err}
}
