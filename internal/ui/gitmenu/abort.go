package gitmenu

import (
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"

	"charm.land/huh/v2"
)

func abortOp(taskID, name string, sequence taskv1.Sequence) operation {
	return operation{
		running: "aborting the " + panel.DescribeSequence(sequence) + " of " + name,
		run: func(clients api.Clients) report {
			result, err := clients.AbortSequence(taskID)

			return func(screen.Inputs) screen.Request { return reportAborted(name, sequence, result, err) }
		},
	}
}

func reportAborted(name string, sequence taskv1.Sequence, result *agentv1.AbortSequenceResponse, err error) screen.Request {
	if err != nil {
		return screen.Report{Summary: fmt.Sprintf("Could not abort the %s of %s", panel.DescribeSequence(sequence), name), Err: err}
	}

	where := "The checkout"
	if branch := result.GetBranch(); branch != "" {
		where = branch
	}

	lines := []string{where + " is back where it was before.", "Komrad saved a copy of the worktree from before the abort at " + result.GetParkedRef() + "."}

	return screen.Note{
		Kind:    screen.Info,
		Summary: fmt.Sprintf("%s: aborted the %s", name, panel.DescribeSequence(result.GetSequence())),
		Detail:  strings.Join(lines, " "),
	}
}

func abortForm(name string, sequence taskv1.Sequence) confirmForm {
	return func(keys config.FixedKeys) (*huh.Form, *bool) {
		return form.Confirm(
			fmt.Sprintf("Abort the %s in the worktree of %s?", panel.DescribeSequence(sequence), name),
			fmt.Sprintf("Komrad puts the branch and its files back where they were before the %s began. "+
				"It first saves a copy of the worktree as it is now, conflicts and all. "+
				"If the agent is working, komrad stops it first, and it tells the agent either way.", panel.DescribeSequence(sequence)),
			"Abort "+panel.DescribeSequence(sequence),
			keys,
		)
	}
}
