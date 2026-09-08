package review

import (
	"weave/internal/git"
)

// RunsFrom turns what a reviewer wrote in the round's checkout into the runs a harvest sends,
// anchoring each against the diff because the numbers and quoted lines a thread is pinned by
// come from the patch rather than from the file.
func RunsFrom(view *View, edits []git.Edit, fallback func(path string) ([]string, bool)) []Run {
	var runs []Run

	for _, edit := range edits {
		round, ok := roundLines(view, edit.Path, fallback)
		if !ok {
			continue
		}

		suggestion, ok := Suggest(edit, round)
		if !ok {
			continue
		}

		run := Run{
			FilePath: suggestion.FilePath,
			NewStart: suggestion.NewStart,
			NewEnd:   suggestion.NewEnd,
			Lines:    suggestion.Lines,
		}

		// A run the diff does not draw has no anchor to take, which is what leaves its thread
		// adrift rather than pinned to a row.
		if anchor, ok := view.GetRangeAnchor(edit.Path, suggestion.NewStart, suggestion.NewEnd); ok {
			run.OldStart = anchor.OldStart
			run.OldEnd = anchor.OldEnd
			run.NewStart = anchor.NewStart
			run.NewEnd = anchor.NewEnd
			run.QuotedLines = anchor.QuotedLines

			// Explainer: a patch measured from somewhere other than the round's own base numbers
			// its old side against that other commit. A thread records no such commit, so an
			// old-side number taken from here would name a line nobody can find again.
			if view.IsOldSideForeign() {
				run.OldStart, run.OldEnd = 0, 0
			}
		}

		runs = append(runs, run)
	}

	return runs
}

// roundLines is the round's own copy of a file, taken off the patch where it draws one and from
// the caller otherwise.
func roundLines(view *View, path string, fallback func(path string) ([]string, bool)) ([]string, bool) {
	if source, ok := view.GetSource(path); ok {
		return SplitSource(source), true
	}

	if fallback == nil {
		return nil, false
	}

	return fallback(path)
}
