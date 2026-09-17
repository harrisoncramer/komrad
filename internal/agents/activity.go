package agents

import "weave/internal/harness"

// Explainer: a frame weave could not match is not evidence of anything, and the two questions
// asked of a reading both have to answer no to it. The keys that stop a CLI mid-turn are keys its
// input box reads for something else when no turn is running, and Claude's are its checkpoint
// shortcut, so sending them on a guess puts a dialog on screen that nobody is at the pane to
// dismiss. A hold is granted on the other question, and an agent nothing has seen stop is one a
// person's editor must not land on top of.

// isMidTurn reports whether a reading says a turn is running.
func isMidTurn(activity harness.Activity) bool {
	switch activity {
	case harness.ActivityWorking, harness.ActivityWaiting:
		return true
	case harness.ActivityIdle, harness.ActivityComposing, harness.ActivityUnrecognized, harness.ActivityUnknown:
		return false
	}

	return false
}

// isAtRest reports whether a reading says the agent has stopped.
func isAtRest(activity harness.Activity) bool {
	switch activity {
	case harness.ActivityIdle, harness.ActivityComposing:
		return true
	case harness.ActivityWaiting, harness.ActivityWorking, harness.ActivityUnrecognized, harness.ActivityUnknown:
		return false
	}

	return false
}
