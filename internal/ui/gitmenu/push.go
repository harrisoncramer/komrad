package gitmenu

import (
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xsha"
	agentv1 "komrad/proto/gen/agent/v1"

	"charm.land/huh/v2"
)

// pushPlan is what a push would do, carried from the server to the confirmation.
type pushPlan struct {
	name    string
	branch  string
	commits int
	// forced says origin's copy holds commits the branch does not, so the push overwrites it.
	forced      bool
	uncommitted int
	// pullRequest is the branch's pull request, whose auto-merge may land what goes up.
	pullRequest panel.PullRequestStanding
}

func pushOp(task panel.Task) operation {
	taskID, name := task.ID, task.Slug

	return operation{
		planning:   "planning the push of " + name,
		planFailed: "Could not work out what a push would do",
		plan: func(clients api.Clients) (confirmForm, error) {
			response, err := clients.PlanPush(taskID)
			if err != nil {
				return nil, err
			}
			if response.GetInSync() {
				return nil, skippedError(name + ": commit already pushed")
			}

			plan := pushPlan{
				name:        name,
				branch:      response.GetBranch(),
				commits:     int(response.GetCommits()),
				forced:      response.GetForced(),
				uncommitted: int(response.GetUncommittedFiles()),
				pullRequest: task.PullRequest,
			}

			return pushForm(plan), nil
		},
		running: "pushing " + name,
		run: func(clients api.Clients) report {
			result, err := clients.PushWorktree(taskID)

			return func(screen.Inputs) screen.Request { return reportPushed(name, result, err) }
		},
	}
}

func reportPushed(name string, result *agentv1.PushWorktreeResponse, err error) screen.Request {
	if err != nil {
		return screen.Report{Summary: "Could not push the branch of " + name, Err: err}
	}

	commits := int(result.GetCommits())
	sha := result.GetSha()

	if result.GetForced() {
		return screen.PostMessage{
			MessageKind: screen.Info,
			Summary:     name + ": force-pushed " + result.GetBranch(),
			Detail: fmt.Sprintf("%s. Komrad pushed %s over origin's copy and told %s.",
				xsha.Short(sha), label.Count(commits, "commit", "commits"), name),
		}
	}

	return screen.PostMessage{
		MessageKind: screen.Info,
		Summary:     name + ": pushed " + result.GetBranch(),
		Detail:      fmt.Sprintf("%s. Komrad pushed %s.", xsha.Short(sha), label.Count(commits, "commit", "commits")),
	}
}

func pushForm(plan pushPlan) confirmForm {
	return func(keys config.FixedKeys) (*huh.Form, *bool) {
		return form.Confirm(
			"Push the branch of "+plan.name+"?",
			pushDescription(plan),
			pushAffirmative(plan),
			keys,
		)
	}
}

// pushDescription says what the push will send, then what it overwrites, then what stays behind.
func pushDescription(plan pushPlan) string {
	lines := []string{fmt.Sprintf("Komrad pushes %s to origin as %s.", label.Count(plan.commits, "commit", "commits"), plan.branch)}

	if plan.forced {
		lines = append(lines, "Origin's copy holds commits this branch dropped, as a rebase does, so komrad overwrites it. "+
			"Komrad refuses the push if origin moved since komrad last read it.")
	}

	if plan.pullRequest.MergeReadiness.AutoMerge {
		lines = append(lines, "Auto-merge is on for its "+plan.pullRequest.Noun()+", so the forge merges the push once its checks and "+
			"reviews pass.")
	}

	if plan.uncommitted > 0 {
		lines = append(lines, fmt.Sprintf("Komrad leaves %s of uncommitted changes behind.",
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
