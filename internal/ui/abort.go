package ui

import (
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/form"
	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

func abortOp(taskID, name string, sequence panel.Sequence) gitOp {
	return gitOp{
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

	lines := []string{where + " is back where it was before.", "Komrad saved a copy of the worktree from before the abort at " + result.GetParkedRef() + "."}
	if result.GetInterrupted() {
		lines = append(lines, "Komrad interrupted its turn first.")
	}

	m.pushMessage(message{
		kind:    messageInfo,
		summary: fmt.Sprintf("%s: aborted the %s", name, panel.SequenceFrom(result.GetSequence())),
		detail:  strings.Join(lines, " "),
	})
}

func abortForm(name string, sequence panel.Sequence) gitConfirmForm {
	return func(keys config.FixedKeys) (*huh.Form, *bool) {
		return form.Confirm(
			fmt.Sprintf("Abort the %s in the worktree of %s?", sequence, name),
			fmt.Sprintf("Komrad puts the branch and its files back where they were before the %s began. "+
				"It first saves a copy of the worktree as it is now, conflicts and all. "+
				"If the agent is working, komrad stops it first, and it tells the agent either way.", sequence),
			"Abort "+string(sequence),
			keys,
		)
	}
}
