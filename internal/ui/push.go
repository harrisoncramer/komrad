package ui

import (
	"fmt"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/git"
	"komrad/internal/ui/paint/label"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// pushTimeout outlasts the server's own limit on git, so the server is the one to give up.
const pushTimeout = 11 * time.Minute

// pushPlan is what a push would do, carried from the server to the confirmation.
type pushPlan struct {
	agentID string
	name    string
	branch  string
	commits int
	// forced says origin's copy holds commits the branch does not, so the push overwrites it.
	forced      bool
	uncommitted int
}

// pushPlannedMsg is a plan come back, or the reason there is none.
type pushPlannedMsg struct {
	plan  pushPlan
	err   error
	write int
}

// pushedMsg is a finished push.
type pushedMsg struct {
	name    string
	branch  string
	commits int
	forced  bool
	sha     string
	err     error
	write   int
}

// planPushCmd asks what a push would do.
func (m *model) planPushCmd(agentID, name string) tea.Cmd {
	clients := m.api
	write := m.beginWrite("planning the push of " + name)

	return func() tea.Msg {
		plan, err := clients.planPush(agentID)
		if err != nil {
			return pushPlannedMsg{err: err, write: write}
		}

		return pushPlannedMsg{write: write, plan: pushPlan{
			agentID:     agentID,
			name:        name,
			branch:      plan.GetBranch(),
			commits:     int(plan.GetCommits()),
			forced:      plan.GetForced(),
			uncommitted: int(plan.GetUncommittedFiles()),
		}}
	}
}

// pushCmd runs the push and reports what it did.
func (m *model) pushCmd(agentID, name string) tea.Cmd {
	clients := m.api
	write := m.beginWrite("pushing " + name)

	return func() tea.Msg {
		result, err := clients.pushWorktree(agentID)
		if err != nil {
			return pushedMsg{
				name:  name,
				err:   err,
				write: write,
			}
		}

		return pushedMsg{
			write:   write,
			name:    name,
			branch:  result.GetBranch(),
			commits: int(result.GetCommits()),
			forced:  result.GetForced(),
			sha:     result.GetSha(),
		}
	}
}

// applyPushPlanned opens the confirmation, or says why there is nothing to confirm.
func (m *model) applyPushPlanned(msg pushPlannedMsg) (tea.Model, tea.Cmd) {
	held := m.landHeld(msg.write)

	if msg.err != nil {
		m.report("Could not work out what a push would do", msg.err)

		if held {
			return m.closeForm()
		}

		return m, nil
	}

	return m.openPushConfirm(msg.plan)
}

// applyPushed reports the outcome, and takes down the confirmation held for it.
func (m *model) applyPushed(msg pushedMsg) (tea.Model, tea.Cmd) {
	held := m.landHeld(msg.write)

	m.reportPushed(msg)

	if held {
		return m.closeForm()
	}

	return m, nil
}

// openPushConfirm asks before sending a branch to origin.
func (m model) openPushConfirm(plan pushPlan) (tea.Model, tea.Cmd) {
	form, confirmed := pushForm(plan, formKeys())

	return m, m.openForm(formState{
		kind:   formAgentPush,
		form:   form,
		update: model.updateForm,
		submit: holdSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			cmd := m.pushCmd(plan.agentID, plan.name)

			return m, cmd
		}),
		rebuild: func(model) (*huh.Form, []string) {
			held, _ := pushForm(plan, formKeys())

			return held, nil
		},
	})
}

func (m *model) reportPushed(msg pushedMsg) {
	if msg.err != nil {
		m.report("Could not push the branch of "+msg.name, msg.err)

		return
	}

	if msg.forced {
		m.pushMessage(message{
			kind:    messageInfo,
			summary: msg.name + ": force-pushed " + msg.branch,
			detail: fmt.Sprintf("%s. %s went up over origin's copy, and %s has been told.",
				git.ShortSHA(msg.sha), label.Count(msg.commits, "commit", "commits"), msg.name),
		})

		return
	}

	m.pushMessage(message{
		kind:    messageInfo,
		summary: msg.name + ": pushed " + msg.branch,
		detail:  fmt.Sprintf("%s. %s went up.", git.ShortSHA(msg.sha), label.Count(msg.commits, "commit", "commits")),
	})
}

func pushForm(plan pushPlan, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		"Push the branch of "+plan.name+"?",
		pushDescription(plan),
		pushAffirmative(plan),
		keys,
	)
}

// pushDescription says what the push will send, then what it overwrites, then what stays behind.
func pushDescription(plan pushPlan) string {
	lines := []string{fmt.Sprintf("%s will go up to origin as %s.", label.Count(plan.commits, "commit", "commits"), plan.branch)}

	if plan.forced {
		lines = append(lines, "Origin's copy holds commits this branch no longer does, as a rebase leaves it, so it will be "+
			"overwritten. The push is refused if origin has moved since komrad last read it.")
	}

	if plan.uncommitted > 0 {
		lines = append(lines, fmt.Sprintf("%s nothing has committed will stay behind.",
			label.Count(plan.uncommitted, "file", "files")))
	}

	return strings.Join(lines, " ")
}

// pushAffirmative names the button for what will actually run.
func pushAffirmative(plan pushPlan) string {
	if plan.forced {
		return "Force push"
	}

	return "Push"
}
