package ui

import (
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

func abortOp(taskID, name string, sequence panel.Sequence) gitOp {
	return gitOp{
		kind:    formAgentAbort,
		running: "aborting the " + string(sequence) + " of " + name,
		run: func(clients api) gitReport {
			result, err := clients.abortSequence(taskID)

			return func(m *model) tea.Cmd {
				m.reportAborted(name, sequence, result, err)

				return nil
			}
		},
	}
}

func (m *model) reportAborted(name string, sequence panel.Sequence, result *agentv1.AbortSequenceResponse, err error) {
	if err != nil {
		m.report(fmt.Sprintf("Could not abort the %s of %s", sequence, name), err)

		return
	}

	where := "The checkout"
	if branch := result.GetBranch(); branch != "" {
		where = branch
	}

	lines := []string{where + " is back where it stood before.", "A copy of the worktree from before the abort is kept at " + result.GetParkedRef() + "."}
	if result.GetInterrupted() {
		lines = append(lines, "Its turn was cut first.")
	}

	m.pushMessage(message{
		kind:    messageInfo,
		summary: fmt.Sprintf("%s: aborted the %s", name, sequenceFrom(result.GetSequence())),
		detail:  strings.Join(lines, " "),
	})
}

func abortForm(name string, sequence panel.Sequence) gitConfirmForm {
	return func(keys config.FixedKeys) (*huh.Form, *bool) {
		return confirmForm(
			fmt.Sprintf("Abort the %s in the worktree of %s?", sequence, name),
			fmt.Sprintf("The branch goes back to where it stood before the %s began, and its files with it. "+
				"A copy of the worktree as it stands now, conflicts and all, is kept first. "+
				"%s is stopped first if it is working, and is told either way.", sequence, name),
			"Abort "+string(sequence),
			keys,
		)
	}
}
