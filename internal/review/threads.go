package review

import (
	agentv1 "weave/proto/gen/agent/v1"
)

// anchorKey is where a thread is pinned, as the view looks it up: the file and the line on
// whichever side the thread lives on. A thread on a deleted line has no new-side number, so
// the side is part of the key rather than assumed.
type anchorKey struct {
	filePath string

	newLine int32
	oldLine int32
}

func keyForThread(thread *agentv1.ReviewThread) anchorKey {
	return anchorKey{
		filePath: thread.GetFilePath(),
		newLine:  thread.GetNewStart(),
		oldLine:  thread.GetOldStart(),
	}
}
