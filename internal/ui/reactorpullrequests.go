package ui

import (
	"time"

	"weave/internal/assemble"
	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	watchv1 "weave/proto/gen/watch/v1"

	tea "github.com/charmbracelet/bubbletea"
)

// pullRequestReactor asks for the pull requests on the branch of each agent that arrives there.
type pullRequestReactor struct {
	seen map[string]agentPlace
}

// agentPlace is the branch an agent was last seen on, and whether it was archived there.
type agentPlace struct {
	repository string
	branch     string
	isArchived bool
}

// pullRequestsListedMsg is the pull requests on the branch of an agent that arrived.
type pullRequestsListedMsg struct {
	pullRequests []*pullrequestv1.PullRequest
	err          error
}

func newPullRequestReactor() *pullRequestReactor {
	return &pullRequestReactor{seen: map[string]agentPlace{}}
}

func (r *pullRequestReactor) react(m *model, changed assemble.Changed) tea.Cmd {
	agents := m.listing.world.Agents
	if changed.Replaced {
		clear(r.seen)
		for id, agent := range agents {
			r.seen[id] = placeAgent(agent)
		}

		return nil
	}

	clients := m.api

	var cmds []tea.Cmd
	for _, id := range changed.Keys[watchv1.Resource_RESOURCE_AGENT] {
		agent, ok := agents[id]
		if !ok {
			delete(r.seen, id)

			continue
		}

		place := placeAgent(agent)
		held, wasSeen := r.seen[id]
		r.seen[id] = place
		if wasSeen && !hasArrived(held, place) {
			continue
		}
		if place.repository == "" || place.branch == "" || place.isArchived {
			continue
		}

		cmds = append(cmds, func() tea.Msg {
			listed, err := clients.listPullRequests(place.repository, place.branch)

			return pullRequestsListedMsg{pullRequests: listed, err: err}
		})
	}

	return tea.Batch(cmds...)
}

// applyPullRequestsListed puts the pull requests answered for a branch into the world.
func (m *model) applyPullRequestsListed(msg pullRequestsListedMsg) tea.Cmd {
	m.report("Could not list the pull requests", msg.err)

	return m.withListing(func(l *listing) {
		for _, pullRequest := range msg.pullRequests {
			l.world.Put(&watchv1.Row{Row: &watchv1.Row_PullRequest{PullRequest: pullRequest}})
		}

		l.assemble(time.Now())
	})
}

func placeAgent(agent *agentv1.Agent) agentPlace {
	return agentPlace{
		repository: agent.GetRepositoryUrl(),
		branch:     agent.GetBranch(),
		isArchived: agent.GetArchivedAt() != "",
	}
}

// hasArrived is whether an agent moved branch or came back from the archive.
func hasArrived(held, place agentPlace) bool {
	isRestored := held.isArchived && !place.isArchived

	return isRestored || held.repository != place.repository || held.branch != place.branch
}
