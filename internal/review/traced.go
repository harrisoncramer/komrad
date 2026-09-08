package review

import agentv1 "weave/proto/gen/agent/v1"

// Traced is where a thread's lines went between the snapshot it was written against and the one
// on screen, as git reports it rather than as a string match guesses it.
type Traced struct {
	// FilePath is the thread's file in the snapshot on screen, which a rename moves.
	FilePath string
	// NewStart is its first line there, zero when the lines themselves are gone.
	NewStart int32
	// FileOnly says the file survived and the lines did not, so the thread belongs to the file
	// rather than to any line of it.
	FileOnly bool
}

// SetTraced holds where each thread's lines went, keyed by thread id, and is read by the next
// PinThreads rather than at once.
func (v *View) SetTraced(traced map[string]Traced) {
	v.traced = traced
}

// pathOf is the file a thread is about in the snapshot on screen, which a rename moves and which
// tracing is the only thing that knows about.
func (v *View) pathOf(thread *agentv1.ReviewThread) string {
	found, ok := v.traced[thread.GetId()]
	if !ok || found.FilePath == "" {
		return thread.GetFilePath()
	}

	return found.FilePath
}

// tracedRowFor is the row git says a thread's lines became, and whether it said anything at all.
func (v *View) tracedRowFor(index lineIndex, thread *agentv1.ReviewThread) (int, bool) {
	found, ok := v.traced[thread.GetId()]
	if !ok || found.FileOnly || found.NewStart <= 0 {
		return 0, false
	}

	y, ok := index.byNew[newKey{filePath: found.FilePath, newLine: found.NewStart}]

	return y, ok
}
