package scriptrun

import (
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
)

const (
	// WorktreeProvisioner is the built-in script that makes a task's worktree.
	WorktreeProvisioner = "_worktree_setup"
	// WorktreeDeprovisioner is the built-in script that gives a task's worktree back.
	WorktreeDeprovisioner = "_worktree_cleanup"
)

// WorktreeScripts is the provisioner and deprovisioner a workflow gives an agent, the built-in pair or nothing at all.
func WorktreeScripts(wf *workflowstore.Workflow, provisionEnabled *bool) (provisioner, deprovisioner string) {
	if wf == nil || !wf.Worktree.Enabled || agentstore.IsProvisionerDisabled(provisionEnabled) {
		return "", ""
	}

	return WorktreeProvisioner, WorktreeDeprovisioner
}
