package agents

import "weave/internal/harness"

// Explainer: a frame weave could not match is not evidence of anything, so both questions asked of
// a reading answer no to it. The keys that stop a CLI mid-turn are keys its input box reads for
// something else when no turn is running, and Claude's are its checkpoint shortcut, so sending
// them on a guess puts a dialog on screen that nobody is at the pane to dismiss.

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

// Explainer: a prompt waiting on an answer is not at rest. It holds a write that lands the moment
// anybody answers it, and it is also the frame an interrupt is sent to clear, so treating it as
// rest would let a hold be granted on the picture it was meant to change.

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
