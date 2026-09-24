package ui

import (
	"context"
	"errors"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"weave/config"
	"weave/internal/ui/panel"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitModel is the agents list with the cursor on an agent whose workflow makes worktrees.
func gitModel(t *testing.T) model {
	t.Helper()

	m := infoModel(&recordingTmux{width: 200, height: 40})
	m.width, m.height = 200, 40
	m.api = driftAPI()
	m.listing.workflows = []panel.Workflow{{
		ID:       "w1",
		Name:     "backend",
		Worktree: panel.WorktreeConfig{Enabled: true},
	}}

	return m
}

func routeKey(t *testing.T, m model, pressed string) model {
	t.Helper()

	next, _ := m.route(keyMsgOf(t, pressed))
	updated, ok := next.(model)
	require.True(t, ok)

	return updated
}

// TestGitMenuOpensOnAnAgent covers the key that draws the menu, and the row it does nothing on.
func TestGitMenuOpensOnAnAgent(t *testing.T) {
	opened := routeKey(t, gitModel(t), "b")
	assert.Equal(t, formGit, opened.form.kind)

	onWorkflow := gitModel(t)
	onWorkflow.agents.cursor = 0
	assert.Equal(t, formNone, routeKey(t, onWorkflow, "b").form.kind,
		"a workflow header has no branch of its own")
}

// TestGitMenuClosesOnEsc covers leaving the menu without picking anything.
func TestGitMenuClosesOnEsc(t *testing.T) {
	closed := routeKey(t, routeKey(t, gitModel(t), "b"), "esc")

	assert.Equal(t, formNone, closed.form.kind)
	assert.Empty(t, closed.messages.items)
}

// TestGitMenuEntriesAnswerTheirOwnKeys covers picking an entry by its letter rather than by
// walking the cursor onto it.
func TestGitMenuEntriesAnswerTheirOwnKeys(t *testing.T) {
	picked := routeKey(t, routeKey(t, gitModel(t), "b"), "r")

	assert.Equal(t, formNone, picked.form.kind)
	assert.Empty(t, picked.messages.items, "the entry answered rather than saying why it could not")
}

// TestGitMenuSaysWhyAnEntryWouldDoNothing covers an agent an entry cannot act on, where the
// menu is still drawn and the reason is read rather than the press being swallowed.
func TestGitMenuSaysWhyAnEntryWouldDoNothing(t *testing.T) {
	tests := []struct {
		name         string
		pressed      string
		standing     panel.PullRequestStanding
		pullRequests []panel.PullRequest
		want         string
	}{
		{
			name:    "the rebase",
			pressed: "r",
			want:    errNoWorktree.Error(),
		},
		{
			name:     "the pull request of a repository not read yet",
			pressed:  "o",
			standing: panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD},
			want:     errPullRequestsUnread.Error(),
		},
		{
			name:     "the pull request of a branch with none",
			pressed:  "o",
			standing: panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE},
			want:     errNoPullRequest.Error(),
		},
		{
			name:         "the pull request of a branch that has moved past its last one",
			pressed:      "o",
			standing:     panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE},
			pullRequests: []panel.PullRequest{{Number: 740, State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED}},
			want:         errMovedPastPullRequest.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := gitModel(t)
			m.listing.workflows[0].Worktree.Enabled = false
			m.listing.agents[0].PullRequest = tt.standing
			m.listing.agents[0].PullRequests = tt.pullRequests

			opened := routeKey(t, m, "b")
			require.Equal(t, formGit, opened.form.kind)

			picked := routeKey(t, opened, tt.pressed)

			assert.Equal(t, formNone, picked.form.kind)
			require.Len(t, picked.messages.items, 1)
			assert.Contains(t, picked.messages.items[0].detail, tt.want)
		})
	}
}

// TestGitMenuOpensTheCurrentPullRequest covers the entry handing the browser the address the
// listing carries for the agent the cursor was on, and saying so when the browser refuses.
func TestGitMenuOpensTheCurrentPullRequest(t *testing.T) {
	tests := []struct {
		name    string
		refuse  error
		wantErr bool
	}{
		{
			name:    "a browser that refuses is reported",
			refuse:  errors.New("no application knows how to open it"),
			wantErr: true,
		},
		{
			name: "the address is handed over",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opener := &recordingOpener{err: tt.refuse}
			m := gitModel(t)
			m.browser = opener
			m.listing.agents[0].PullRequest = panel.PullRequestStanding{
				State:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
				Number: 740,
				URL:    "https://github.com/harrisoncramer/weave/pull/740",
			}

			opened := routeKey(t, m, "b")
			require.Equal(t, formGit, opened.form.kind)

			next, cmd := opened.route(keyMsgOf(t, "o"))
			picked, ok := next.(model)
			require.True(t, ok)
			assert.Equal(t, formNone, picked.form.kind)
			require.NotNil(t, cmd, "the browser is asked off the update")

			for _, msg := range produced(cmd) {
				updated, _ := picked.Update(msg)
				picked, ok = updated.(model)
				require.True(t, ok)
			}

			assert.Equal(t, []string{"https://github.com/harrisoncramer/weave/pull/740"}, opener.targets)
			if tt.wantErr {
				require.Len(t, picked.messages.items, 1)
				assert.Contains(t, picked.messages.items[0].summary, "Could not open")

				return
			}
			assert.Empty(t, picked.messages.items)
		})
	}
}

// recordingOpener stands in for the desktop, holding what it was asked to open.
type recordingOpener struct {
	targets []string
	err     error
}

func (r *recordingOpener) OpenURL(_ context.Context, target string) error {
	r.targets = append(r.targets, target)

	return r.err
}

// TestGitMenuEntriesNameTheirKeysAndTheirRefusals covers the labels, which carry both the letter
// that picks an entry and the reason it would do nothing on this agent.
func TestGitMenuEntriesNameTheirKeysAndTheirRefusals(t *testing.T) {
	entry := gitEntry{
		name:    "Rebase onto the base",
		keys:    config.DefaultKeybindings().Git.Rebase,
		refusal: "no worktree of its own",
		held:    true,
	}

	assert.Equal(t, "Rebase onto the base (r)", gitEntryLabel(entry))

	entry.held = false
	assert.Equal(t, "Rebase onto the base (r) · no worktree of its own", gitEntryLabel(entry))

	entry.keys = nil
	assert.Equal(t, "Rebase onto the base · no worktree of its own", gitEntryLabel(entry))
}

// TestGitMenuDrawsEveryEntry covers the menu on an agent the entry cannot act on, where the
// select would otherwise scroll it out of the column.
func TestGitMenuDrawsEveryEntry(t *testing.T) {
	m := gitModel(t)
	m.listing.workflows[0].Worktree.Enabled = false

	drawn := ansi.Strip(routeKey(t, m, "b").render())

	assert.Contains(t, drawn, "Rebase onto the base")
	assert.Contains(t, drawn, "Open the pull request")
	assert.Contains(t, drawn, "Cancel")
}

// TestGitMenuDoesNotFilter covers the select's own filter, which would otherwise take the letters
// the entries answer to.
func TestGitMenuDoesNotFilter(t *testing.T) {
	opened := routeKey(t, gitModel(t), "b")
	assert.NotContains(t, ansi.Strip(opened.render()), "filter", "the footer offers no filter to type into")

	typed := routeKey(t, opened, "/")

	assert.Equal(t, formGit, typed.form.kind)
	assert.Contains(t, ansi.Strip(typed.render()), "Rebase onto the base", "the entries are as they were")
}

// TestGitMenuActsOnTheAgentItWasOpenedOn covers the row leaving the list while the menu stands,
// where the entry has nothing left to act on.
func TestGitMenuActsOnTheAgentItWasOpenedOn(t *testing.T) {
	opened := routeKey(t, gitModel(t), "b")
	opened.listing.agents = nil

	picked := routeKey(t, opened, "r")

	assert.Equal(t, formNone, picked.form.kind)
	require.Len(t, picked.messages.items, 1)
	assert.Contains(t, picked.messages.items[0].detail, errAgentGone.Error())
}

// TestPullRequestHint covers the menu's hint beside the pull request entry for each reason an
// agent has none to open.
func TestPullRequestHint(t *testing.T) {
	none := panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE}

	tests := []struct {
		name string
		item panel.AgentRow
		want string
	}{
		{
			name: "a repository not read yet",
			item: panel.AgentRow{PullRequest: panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD}},
			want: "not read yet",
		},
		{
			name: "a branch nothing was opened from",
			item: panel.AgentRow{PullRequest: none},
			want: "none from this branch",
		},
		{
			name: "a branch that has moved past its last pull request",
			item: panel.AgentRow{
				PullRequest:  none,
				PullRequests: []panel.PullRequest{{Number: 740, State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED}},
			},
			want: "moved on since its last one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pullRequestHint(pullRequestRefusal(tt.item)))
		})
	}
}
