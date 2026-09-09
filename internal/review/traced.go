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

// PathOf is the file a thread is about in the snapshot on screen, which a rename moves.
func (v *View) PathOf(thread *agentv1.ReviewThread) string {
	found, ok := v.traced[thread.GetId()]
	if ok && found.FilePath != "" {
		return found.FilePath
	}

	return v.renamedTo(thread.GetFilePath())
}

// renamedTo is the path the diff draws a file under, which reads the rename out of the patch
// itself so a thread is still placeable in the cases tracing answers for nothing.
func (v *View) renamedTo(filePath string) string {
	if _, drawn := v.findFile(filePath); drawn {
		return filePath
	}

	for fileIndex := range v.patch.Files {
		file := &v.patch.Files[fileIndex]
		if file.OldPath == filePath && file.NewPath != "" {
			return file.NewPath
		}
	}

	return filePath
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
