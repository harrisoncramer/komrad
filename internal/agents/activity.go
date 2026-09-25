package agents

import "komrad/internal/harness"

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
