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
	actionTaskRename
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
	actionDocumentsBack
	actionDocumentsOlder
	actionDocumentsNewer
	actionLogsBack
	actionLogsCopy
	actionStatusChecksBack
	actionStatusChecksLogs
	actionStatusCheckLogsBack
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
)
