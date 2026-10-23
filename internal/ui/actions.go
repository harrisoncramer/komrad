package ui

import "komrad/internal/ui/keys"

// The actions the root's screens declare of their own.
const (
	actionAgentsBack keys.ID = keys.Own + iota
	actionAgentTriage
	actionAgentOpen
	actionAgentRename
	actionTaskRename
	actionTaskNotesBack
	actionWorkflowRename
	actionArchiveRename
	actionArchiveBack
	actionWorktreesBack
	actionDocumentsBack
	actionDocumentsOlder
	actionDocumentsNewer
	actionDiffFile
	actionDiffMatch
	actionDiffPan
	actionDiffPanReset
	actionDiffWrap
	actionDiffExpand
	actionReviewBack
	actionReviewThread
	actionReviewOpenThread
	actionReviewBase
	actionReviewBaseForward
	actionReviewFolder
	actionReviewSide
)
