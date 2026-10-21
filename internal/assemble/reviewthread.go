package assemble

import (
	"cmp"
	"slices"
	"strings"

	agentv1 "komrad/proto/gen/agent/v1"
)

// ReviewThreads is the conversation on the diff of an agent's task.
func ReviewThreads(w World, agentID string) []*agentv1.ReviewThread {
	taskID := w.Agents[agentID].GetTaskId()
	if taskID == "" {
		return nil
	}

	var threads []*agentv1.ReviewThread
	for _, thread := range w.ReviewThreads {
		if thread.GetTaskId() == taskID {
			threads = append(threads, thread)
		}
	}

	slices.SortFunc(threads, compareThreads)

	return threads
}

// compareThreads orders threads by snapshot, then by where they sit in the diff.
func compareThreads(a, b *agentv1.ReviewThread) int {
	return cmp.Or(
		cmp.Compare(a.GetSnapshotOrdinal(), b.GetSnapshotOrdinal()),
		strings.Compare(a.GetFilePath(), b.GetFilePath()),
		cmp.Compare(a.GetNewStart(), b.GetNewStart()),
		cmp.Compare(a.GetOldStart(), b.GetOldStart()),
		strings.Compare(a.GetId(), b.GetId()),
	)
}
