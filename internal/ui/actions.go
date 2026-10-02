package ui

// actionID names one thing a screen does with a key.
type actionID int

const (
	actionNone actionID = iota
	actionHelp
	actionMove
	actionJump
	actionNavPage
	actionColumnScroll
	actionColumnBack
	actionDetails
	actionDetailsCopy
	actionMessages
	actionFilterOpen
	actionFilterClear
	actionFilterMatch
	actionQuit
	actionAgentsBack
	actionAgentTriage
	actionAgentOpen
	actionAgentRename
	actionSearchOpen
	actionSearchEdit
	actionSearchBack
	actionWorkflowRename
	actionSelectToggle
	actionSelectRange
	actionSelectClear
	actionScheduleRename
	actionScheduleBack
	actionArchiveRename
	actionArchiveBack
	actionWorktreesBack
	actionWorktreesJumpToAgent
	actionDocumentsBack
	actionDocumentsOlder
	actionDocumentsNewer
	actionLogsBack
	actionLogsCopy
	actionDiffFile
	actionDiffMatch
	actionDiffPan
	actionDiffPanReset
	actionDiffExpand
	actionReviewBack
	actionReviewThread
	actionReviewOpenThread
	actionReviewEdit
	actionReviewBase
	actionReviewReload
	actionReviewResolve
	actionReviewViewed
	actionReviewBaseForward
	actionReviewAnswer
	actionReviewJump
	actionReviewCompose
	actionReviewSend
)
