package claude

import (
	"weave/internal/harness"
)

// ReadActivity reports what Claude is doing, from what Claude says about itself rather
// than from the chrome it drew. The captured frame is not read at all.
//
// A conversation no running process holds is a finished agent. That covers the ordinary
// ending, and it covers a process that died under a pane outliving it, which is the case
// the panel most needs to be right about. It also covers the second between a launch and
// the first status file, which is why the verdict is left unreported and made to wait out
// the reconciler's debounce.
func (h *Harness) ReadActivity(obs harness.Observation) harness.Reading {
	// An agent with no conversation recorded against it yet is nothing this can answer
	// for, since the status files are keyed by conversation.
	if obs.SessionID == "" {
		return harness.Reading{Activity: harness.ActivityUnknown}
	}

	status, running := h.sessions.status(obs.SessionID)
	if !running {
		return harness.Reading{Activity: harness.ActivityIdle}
	}

	// A status this build has no name for still means a process is holding the
	// conversation, so the agent keeps the state it has rather than being called idle.
	activity, known := statusActivity(status)
	if !known {
		return harness.Reading{Activity: harness.ActivityUnknown}
	}

	return harness.Reading{Activity: activity, Reported: true}
}
