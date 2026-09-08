package ui

import (
	"context"
	"time"

	"weave/internal/git"
	"weave/internal/position"
	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"
)

// traceTimeout is how long every pair together may take, kept apart from the load's own budget
// so a slow tree cannot answer for some pairs and not others.
const traceTimeout = 10 * time.Second

// traceThreadsParams says which conversation to trace, where to read the repository, and which
// snapshot the diff on screen was drawn to.
type traceThreadsParams struct {
	Dir     string
	ToSHA   string
	Threads []*agentv1.ReviewThread
	Rounds  []*agentv1.ReviewRound
}

// traceThreads asks git where each thread's lines went between the snapshot it was written
// against and the one on screen, so placement reads two commits rather than guessing from text.
func traceThreads(ctx context.Context, params traceThreadsParams) map[string]review.Traced {
	if params.ToSHA == "" {
		return nil
	}

	byFrom := groupBySnapshot(params)
	if len(byFrom) == 0 {
		return nil
	}

	// Explainer: the pairs are all or nothing. Sharing the load's deadline let it land part way
	// through, which placed the threads of whichever pairs had answered and sent the rest to the
	// foot, so two identical loads drew the same comments in different places.
	ctx, cancel := context.WithTimeout(ctx, traceTimeout)
	defer cancel()

	traced := map[string]review.Traced{}
	for from, threads := range byFrom {
		mappings, err := git.MapRanges(ctx, git.MapRangesParams{
			Dir:     params.Dir,
			FromSHA: from,
			ToSHA:   params.ToSHA,
		})
		if err != nil {
			return nil
		}

		for id, found := range traceOne(traceOneParams{Mappings: mappings, Threads: threads}) {
			traced[id] = found
		}
	}

	return traced
}

// groupBySnapshot gathers the threads worth tracing under the snapshot each was written against,
// leaving out the ones the screen will not draw.
func groupBySnapshot(params traceThreadsParams) map[string][]*agentv1.ReviewThread {
	snapshots := map[string]string{}
	ordinals := map[string]int32{}
	ceiling := int32(0)

	for _, round := range params.Rounds {
		snapshots[round.GetId()] = round.GetSnapshotSha()
		ordinals[round.GetId()] = round.GetOrdinal()

		if round.GetSnapshotSha() == params.ToSHA {
			ceiling = round.GetOrdinal()
		}
	}

	byFrom := map[string][]*agentv1.ReviewThread{}
	for _, thread := range params.Threads {
		from := snapshots[thread.GetRoundId()]

		// A thread written against the snapshot on screen already answers to its own coordinates,
		// and one whose round nobody can name has nothing to be traced from.
		if from == "" || from == params.ToSHA {
			continue
		}

		// A round later than the one on screen is not drawn there, so tracing it would run a
		// backwards diff of the whole tree for an answer nothing reads.
		if ceiling > 0 && ordinals[thread.GetRoundId()] > ceiling {
			continue
		}

		byFrom[from] = append(byFrom[from], thread)
	}

	return byFrom
}

// traceOneParams is one pair of snapshots and the threads written against the earlier one.
type traceOneParams struct {
	Mappings []position.Mapping
	Threads  []*agentv1.ReviewThread
}

// traceOne moves every thread of one snapshot pair, keeping the file when the lines are gone so a
// thread whose code was rewritten still says which file it was about.
func traceOne(params traceOneParams) map[string]review.Traced {
	positions := make([]position.Position, 0, len(params.Threads))
	for _, thread := range params.Threads {
		positions = append(positions, position.Position{
			Path:  thread.GetFilePath(),
			Lines: threadRange(thread),
		})
	}

	results := position.Transform(position.TransformParams{
		Positions: positions,
		Mappings:  params.Mappings,
		Policy:    position.FileOnConflict,
	})

	traced := map[string]review.Traced{}
	for _, result := range results {
		if result.Index < 0 || result.Index >= len(params.Threads) {
			continue
		}

		id := params.Threads[result.Index].GetId()

		// A file copied to two paths answers twice, and the first answer is kept rather than
		// drawing one comment against whichever copy came last.
		if _, held := traced[id]; held {
			continue
		}

		traced[id] = tracedOf(result)
	}

	return traced
}

// tracedOf reads one result the way the view needs it.
func tracedOf(result position.Result) review.Traced {
	if result.Outcome != position.Placed || result.Position.IsFileLevel() {
		return review.Traced{
			FilePath: result.Position.Path,
			FileOnly: true,
		}
	}

	return review.Traced{
		FilePath: result.Position.Path,
		NewStart: int32(result.Position.Lines.Start),
	}
}

// threadRange is a thread's new-side lines as a half-open span, and nothing for a thread on a
// line the newer side never held.
func threadRange(thread *agentv1.ReviewThread) position.Range {
	start := thread.GetNewStart()
	if start <= 0 {
		return position.Range{}
	}

	end := thread.GetNewEnd()
	if end < start {
		end = start
	}

	return position.Range{Start: int(start), End: int(end) + 1}
}
