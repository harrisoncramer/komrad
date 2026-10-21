package assemble_test

import (
	"testing"

	"komrad/internal/assemble"
	"komrad/lib/xslice"

	agentv1 "komrad/proto/gen/agent/v1"
	watchv1 "komrad/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
)

// TestReviewThreadsRunsDownTheDiff pins the order: the threads of one agent's task, by snapshot, then file, then
// line.
func TestReviewThreadsRunsDownTheDiff(t *testing.T) {
	var w assemble.World
	w.Apply(put(agentRow(agent("a1"))))
	for _, thread := range []*agentv1.ReviewThread{
		threadOf("t4", "a1", 2, "a.go", 1),
		threadOf("t3", "a1", 1, "b.go", 1),
		threadOf("t2", "a1", 1, "a.go", 9),
		threadOf("t1", "a1", 1, "a.go", 3),
		threadOf("t5", "a2", 1, "a.go", 1),
	} {
		w.Put(&watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: thread}})
	}

	ids := xslice.Map(assemble.ReviewThreads(w, "a1"), func(thread *agentv1.ReviewThread) string { return thread.GetId() })

	assert.Equal(t, []string{"t1", "t2", "t3", "t4"}, ids)
}

func threadOf(id, agentID string, snapshot int32, file string, line int32) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:              id,
		TaskId:          "task-" + agentID,
		SnapshotOrdinal: snapshot,
		FilePath:        file,
		NewStart:        line,
	}
}
