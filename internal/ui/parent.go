package ui

import (
	taskv1 "komrad/proto/gen/task/v1"
)

// cascadeBlockSaid is what blocks a child's cascade in words, empty while nothing blocks it.
func cascadeBlockSaid(block taskv1.CascadeBlock, parent string) string {
	switch block {
	case taskv1.CascadeBlock_CASCADE_BLOCK_CONFLICTED:
		return "behind " + parent + ", conflicts"
	case taskv1.CascadeBlock_CASCADE_BLOCK_ORPHANED:
		return "orphaned from " + parent
	case taskv1.CascadeBlock_CASCADE_BLOCK_PARENT_CLOSED:
		return "on " + parent + ", closed unmerged"
	case taskv1.CascadeBlock_CASCADE_BLOCK_UNSPECIFIED:
	}

	return ""
}
