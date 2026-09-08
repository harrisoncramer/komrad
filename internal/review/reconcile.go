package review

import (
	"slices"
	"strings"
)

// Run is one run of edited lines the reviewer wrote into the round's checkout, already anchored
// against the diff so that the numbers here are the ones a thread is pinned by.
type Run struct {
	FilePath    string
	OldStart    int
	OldEnd      int
	NewStart    int
	NewEnd      int
	QuotedLines []string
	Lines       []string
}

// Pinned is a harvested thread of the round being reconciled, which weave may move or delete
// because it wrote the thread itself and nobody has answered in it.
type Pinned struct {
	ID       string
	FilePath string
	NewStart int
	NewEnd   int
	Lines    []string
}

// Frozen is a thread the reconciliation must leave alone, which is every thread released,
// answered, or written by somebody rather than harvested.
type Frozen struct {
	FilePath string
	NewStart int
	NewEnd   int
}

// Move repins one harvested thread onto the run it now stands for.
type Move struct {
	ID  string
	Run Run
}

// Plan is what one harvest does to the round's harvested threads, worked out before anything is
// written so that the whole of it can go in one transaction.
type Plan struct {
	Open    []Run
	Move    []Move
	Delete  []string
	Skipped []Run
}

// IsEmpty reports whether the harvest changes nothing, which is the ordinary answer when the
// reviewer opened the editor and wrote nothing.
func (p Plan) IsEmpty() bool {
	return len(p.Open) == 0 && len(p.Move) == 0 && len(p.Delete) == 0
}

// pair is one run that could stand for one harvested thread, ranked by how much of the two
// overlaps so that the closest reading of what the reviewer did is taken first.
type pair struct {
	run     int
	pinned  int
	overlap int
}

// Reconcile works out what a harvest does to the threads already standing on the round, matching
// each run to the harvested thread it overlaps most and leaving frozen threads and what touches
// them alone.
func Reconcile(runs []Run, pinned []Pinned, frozen []Frozen) Plan {
	var plan Plan

	takenRun := make([]bool, len(runs))
	takenPinned := make([]bool, len(pinned))

	// A run over a thread that has gone out is not weave's to rewrite, and it protects the
	// harvested threads beside it too.
	for i, run := range runs {
		if !isFrozenOver(run, frozen) {
			continue
		}

		takenRun[i] = true
		plan.Skipped = append(plan.Skipped, run)

		for j, thread := range pinned {
			if overlapOf(run.NewStart, run.NewEnd, thread.NewStart, thread.NewEnd) > 0 &&
				run.FilePath == thread.FilePath {
				takenPinned[j] = true
			}
		}
	}

	for _, match := range rankPairs(runs, pinned, takenRun, takenPinned) {
		if takenRun[match.run] || takenPinned[match.pinned] {
			continue
		}

		takenRun[match.run] = true
		takenPinned[match.pinned] = true

		run, thread := runs[match.run], pinned[match.pinned]
		if thread.NewStart == run.NewStart && thread.NewEnd == run.NewEnd &&
			slices.Equal(thread.Lines, run.Lines) {
			continue
		}

		plan.Move = append(plan.Move, Move{ID: thread.ID, Run: run})
	}

	for i, run := range runs {
		if !takenRun[i] {
			plan.Open = append(plan.Open, run)
		}
	}

	for j, thread := range pinned {
		if !takenPinned[j] {
			plan.Delete = append(plan.Delete, thread.ID)
		}
	}

	return plan
}

// rankPairs is every run and harvested thread that could stand for one another, most overlapping
// first, with the rest of the order fixed so that a tie is settled the same way twice.
func rankPairs(runs []Run, pinned []Pinned, takenRun, takenPinned []bool) []pair {
	var pairs []pair

	for i, run := range runs {
		if takenRun[i] {
			continue
		}

		for j, thread := range pinned {
			if takenPinned[j] || thread.FilePath != run.FilePath {
				continue
			}

			overlap := overlapOf(run.NewStart, run.NewEnd, thread.NewStart, thread.NewEnd)
			if overlap == 0 {
				continue
			}

			pairs = append(pairs, pair{
				run:     i,
				pinned:  j,
				overlap: overlap,
			})
		}
	}

	slices.SortFunc(pairs, func(a, b pair) int {
		if a.overlap != b.overlap {
			return b.overlap - a.overlap
		}

		if runs[a.run].NewStart != runs[b.run].NewStart {
			return runs[a.run].NewStart - runs[b.run].NewStart
		}

		if pinned[a.pinned].NewStart != pinned[b.pinned].NewStart {
			return pinned[a.pinned].NewStart - pinned[b.pinned].NewStart
		}

		return strings.Compare(pinned[a.pinned].ID, pinned[b.pinned].ID)
	})

	return pairs
}

// isFrozenOver reports whether a run touches a thread that has already gone out or been answered.
func isFrozenOver(run Run, frozen []Frozen) bool {
	for _, thread := range frozen {
		if thread.FilePath != run.FilePath {
			continue
		}

		if overlapOf(run.NewStart, run.NewEnd, thread.NewStart, thread.NewEnd) > 0 {
			return true
		}
	}

	return false
}

// overlapOf is how many lines two ranges share, which is what decides the thread a run stands for.
func overlapOf(aStart, aEnd, bStart, bEnd int) int {
	return max(0, min(aEnd, bEnd)-max(aStart, bStart)+1)
}
