package scenario

import workflowv1 "weave/proto/gen/workflow/v1"

// Empty is a weave nobody has used yet, which is the first screen anybody sees and the one a cast
// of twelve agents never shows.
func Empty() Scenario {
	return Scenario{
		Name:  "empty",
		Blurb: "a weave with nothing in it yet",
		Workflows: []*workflowv1.Workflow{
			{
				Id: "wf-weave", Name: "weave itself", Cwd: "~/weave", Harness: "claude",
				Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
			},
		},
	}
}
