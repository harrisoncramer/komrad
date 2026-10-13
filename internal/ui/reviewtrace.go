package ui

import (
	"context"
	"fmt"
	"maps"
	"time"

	"komrad/internal/git"
	"komrad/internal/position"
	"komrad/internal/ui/diffview"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// traceTimeout is how long every pair together may take, kept apart from the load's own budget
// so a slow tree cannot answer for some pairs and not others.
const traceTimeout = 10 * time.Second

// traceThreadsParams says which conversation to trace, where to read the repository, and which
// snapshot the diff on screen was rendered to.
type traceThreadsParams struct {
	Git     ReviewGit
	Dir     string
	ToSHA   string
	Threads []*agentv1.ReviewThread
	Rounds  []*agentv1.ReviewRound
}

// traceThreads asks git where each thread's lines went between the snapshot it was written
// against and the one on screen, so placement reads two commits rather than guessing from text.
func traceThreads(ctx context.Context, params traceThreadsParams) map[string]diffview.Traced {
	if params.ToSHA == "" {
		return nil
	}

	exact := traceOnScreen(params)

	byFrom := groupBySnapshot(params)
	if len(byFrom) == 0 {
		return exact
	}

	ctx, cancel := context.WithTimeout(ctx, traceTimeout)
	defer cancel()

	traced := maps.Clone(exact)
	for from, threads := range byFrom {
		found, err := traceAnchors(ctx, traceAnchorsParams{
			Git:     params.Git,
			Dir:     params.Dir,
			FromSHA: from,
			ToSHA:   params.ToSHA,
			Anchors: xslice.Map(threads, traceAnchorOfThread),
		})
		if err != nil {
			return exact
		}

		maps.Copy(traced, found)
	}

	return traced
}

// traceOnScreen places the threads whose numbers the snapshot on screen counted.
func traceOnScreen(params traceThreadsParams) map[string]diffview.Traced {
	onScreen := map[string]bool{}

	for _, round := range params.Rounds {
		if round.GetSnapshotSha() == params.ToSHA {
			onScreen[round.GetId()] = true
		}
	}

	traced := map[string]diffview.Traced{}
	for _, thread := range params.Threads {
		if !onScreen[thread.GetRoundId()] || thread.GetNewStart() <= 0 {
			continue
		}

		traced[thread.GetId()] = tracedExactly(thread.GetFilePath(), thread.GetNewStart())
	}

	return traced
}

// groupBySnapshot gathers the threads worth tracing under the snapshot each was written against,
// leaving out the ones the screen will not render.
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

		// A round later than the one on screen is not rendered there, so tracing it would run a
		// backwards diff of the whole tree for an answer nothing reads.
		if ceiling > 0 && ordinals[thread.GetRoundId()] > ceiling {
			continue
		}

		byFrom[from] = append(byFrom[from], thread)
	}

	return byFrom
}

// traceAnchor is one place written against an earlier snapshot, under the id of the thread or forge
// comment written there.
type traceAnchor struct {
	ID       string
	Position position.Position
}

// traceAnchorOfThread is a thread's new-side lines as a place to trace.
func traceAnchorOfThread(thread *agentv1.ReviewThread) traceAnchor {
	return traceAnchor{
		ID: thread.GetId(),
		Position: position.Position{
			Path:  thread.GetFilePath(),
			Lines: threadRange(thread),
		},
	}
}

// traceAnchorsParams is one pair of snapshots and the places written against the earlier one.
type traceAnchorsParams struct {
	Git     ReviewGit
	Dir     string
	FromSHA string
	ToSHA   string
	Anchors []traceAnchor
}

// traceAnchors moves every place of one snapshot pair, keeping the file when the lines are gone.
func traceAnchors(ctx context.Context, params traceAnchorsParams) (map[string]diffview.Traced, error) {
	mappings, err := params.Git.MapRanges(ctx, git.MapRangesParams{
		Dir:     params.Dir,
		FromSHA: params.FromSHA,
		ToSHA:   params.ToSHA,
		Paths: xslice.Map(params.Anchors, func(anchor traceAnchor) string {
			return anchor.Position.Path
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to map ranges %s..%s: %w", params.FromSHA, params.ToSHA, err)
	}

	results := position.Transform(position.TransformParams{
		Positions: xslice.Map(params.Anchors, func(anchor traceAnchor) position.Position {
			return anchor.Position
		}),
		Mappings: mappings,
		Policy:   position.FileOnConflict,
	})

	traced := map[string]diffview.Traced{}
	for _, result := range results {
		if result.Index < 0 || result.Index >= len(params.Anchors) {
			continue
		}

		id := params.Anchors[result.Index].ID

		// A file copied to two paths answers twice, and the first answer is kept rather than
		// rendering one comment against whichever copy came last.
		if _, held := traced[id]; held {
			continue
		}

		traced[id] = tracedOf(result)
	}

	return traced, nil
}

// tracedExactly is a place whose numbers the snapshot on screen counted.
func tracedExactly(filePath string, newStart int32) diffview.Traced {
	return diffview.Traced{
		FilePath: filePath,
		NewStart: newStart,
		Exact:    true,
	}
}

// tracedOf reads one result the way the view needs it.
func tracedOf(result position.Result) diffview.Traced {
	if result.Outcome != position.Placed || result.Position.IsFileLevel() {
		return diffview.Traced{
			FilePath: result.Position.Path,
			FileOnly: true,
		}
	}

	return diffview.Traced{
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
