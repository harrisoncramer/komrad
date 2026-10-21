package history

import (
	"context"
	"maps"
	"time"

	"komrad/internal/git"
	"komrad/internal/position"
	"komrad/lib/xslice"
)

// traceTimeout caps tracing every thread of one history together.
const traceTimeout = 10 * time.Second

// WidenedReason is why a history diffs the whole change instead of the span asked for.
type WidenedReason int

const (
	// WidenedAnswerGone is an answer whose snapshots are gone.
	WidenedAnswerGone WidenedReason = iota
	// WidenedCommentSnapshotGone is a comment whose snapshot is gone.
	WidenedCommentSnapshotGone
	// WidenedRangeSnapshotGone is a range with a snapshot gone at either end.
	WidenedRangeSnapshotGone
	// WidenedRangeBackwards is a range starting on or after the snapshot it ends on.
	WidenedRangeBackwards
	// WidenedAnswerAcrossRebase is an answer whose older side would not replay across a rebase.
	WidenedAnswerAcrossRebase
)

// Widening is why a history diffs the whole change, with the paths that stopped a replay.
type Widening struct {
	Reason    WidenedReason
	Conflicts []string
}

type Git interface {
	CountChangedFiles(ctx context.Context, params git.CountChangedFilesParams) (int, error)
	Replay(ctx context.Context, params git.ReplayParams) (*git.Replayed, error)
	IsAncestor(ctx context.Context, params git.IsAncestorParams) (bool, error)
}

type Snapshot struct {
	ID      string
	Ordinal int32
	BaseSHA string
	SHA     string
}

// Comment is one comment the person released, on the snapshot its thread is pinned to.
type Comment struct {
	SnapshotID      string
	SnapshotOrdinal int32
	ReleasedAt      time.Time
}

type Thread struct {
	ID         string
	SnapshotID string
	FilePath   string
	NewStart   int32
	NewEnd     int32
}

type Span struct {
	Snapshots *SnapshotSpan
	Answer    *AnswerSpan
	Origin    *OriginSpan
}

type AnswerSpan struct {
	FromSnapshotID string
	ToSnapshotID   string
}

type SnapshotSpan struct {
	FromSnapshotID string
	ToSnapshotID   string
}

type OriginSpan struct {
	SnapshotID string
	BaseSHA    string
}

type ResolvedSpan struct {
	BaseSHA               string
	HeadSHA               string
	Replayed              bool
	AnswerBaseSHA         string
	AnswerHeadSHA         string
	OriginBaseSHA         string
	OriginHeadSHA         string
	OriginSnapshotBaseSHA string
	Conflicts             []string
}

type Record struct {
	Span                ResolvedSpan
	Widened             *Widening
	Earlier             *Snapshot
	EarlierReleasedAt   *time.Time
	ChangedSinceEarlier int
	Traced              map[string]Place
}

type BuildParams struct {
	Git       Git
	Tracer    *Tracer
	Dir       string
	Exclude   []string
	Snapshot  Snapshot
	Snapshots []Snapshot
	Comments  []Comment
	Threads   []Thread
	Span      Span
}

// Build measures the span asked for against the snapshot on screen and traces where each thread's lines now sit.
func Build(ctx context.Context, params BuildParams) Record {
	span, widened := resolveSpan(ctx, params)

	record := Record{Span: span, Widened: widened}
	if widened != nil {
		record.Span = wholeSpan(params.Snapshot)
	}

	record.Earlier, record.EarlierReleasedAt = findPreviousReviewedSnapshot(params.Comments, params.Snapshots, params.Snapshot)
	record.ChangedSinceEarlier = countChangedSince(ctx, countChangedSinceParams{
		Git:     params.Git,
		Dir:     params.Dir,
		Exclude: params.Exclude,
		Earlier: record.Earlier,
		ToSHA:   params.Snapshot.SHA,
	})

	record.Traced = traceThreads(ctx, traceThreadsParams{
		Tracer:    params.Tracer,
		Dir:       params.Dir,
		ToSHA:     record.Span.HeadSHA,
		Threads:   params.Threads,
		Snapshots: params.Snapshots,
	})

	return record
}

func wholeSpan(snapshot Snapshot) ResolvedSpan {
	return ResolvedSpan{BaseSHA: snapshot.BaseSHA, HeadSHA: snapshot.SHA}
}

func resolveSpan(ctx context.Context, params BuildParams) (ResolvedSpan, *Widening) {
	asked := params.Span
	if snapshots := asked.Snapshots; snapshots != nil {
		return resolveSnapshots(ctx, params, *snapshots)
	}

	span := wholeSpan(params.Snapshot)

	var fromBase, toBase string
	if answer := asked.Answer; answer != nil {
		from, to := findSnapshot(params.Snapshots, answer.FromSnapshotID), findSnapshot(params.Snapshots, answer.ToSnapshotID)
		if from == nil || to == nil {
			return ResolvedSpan{}, &Widening{Reason: WidenedAnswerGone}
		}

		from = findAnswerBase(params.Snapshots, from, to)

		span.AnswerBaseSHA, span.AnswerHeadSHA = from.SHA, to.SHA
		span.BaseSHA, span.HeadSHA = span.AnswerBaseSHA, span.AnswerHeadSHA
		fromBase, toBase = from.BaseSHA, to.BaseSHA
	}

	if origin := asked.Origin; origin != nil {
		snapshot := findSnapshot(params.Snapshots, origin.SnapshotID)
		if snapshot == nil {
			return ResolvedSpan{}, &Widening{Reason: WidenedCommentSnapshotGone}
		}

		span.OriginBaseSHA, span.OriginHeadSHA, span.OriginSnapshotBaseSHA = origin.BaseSHA, snapshot.SHA, snapshot.BaseSHA
		if span.OriginBaseSHA == "" {
			span.OriginBaseSHA = snapshot.BaseSHA
		}
		span.BaseSHA, span.HeadSHA = span.OriginBaseSHA, span.OriginHeadSHA
	}

	compared := compareSnapshots(ctx, compareSnapshotsParams{
		Git:      params.Git,
		Dir:      params.Dir,
		FromSHA:  span.BaseSHA,
		FromBase: fromBase,
		ToBase:   toBase,
	})
	if !compared.Comparable {
		return ResolvedSpan{}, &Widening{Reason: WidenedAnswerAcrossRebase, Conflicts: compared.Conflicts}
	}

	span.BaseSHA, span.Replayed = compared.FromSHA, compared.Replayed

	return span, nil
}

// resolveSnapshots measures the work between two snapshots, from the older one as it was when its
// replay onto the newer base conflicts.
func resolveSnapshots(ctx context.Context, params BuildParams, snapshots SnapshotSpan) (ResolvedSpan, *Widening) {
	to := &params.Snapshot
	if snapshots.ToSnapshotID != "" {
		to = findSnapshot(params.Snapshots, snapshots.ToSnapshotID)
	}
	if to == nil {
		return ResolvedSpan{}, &Widening{Reason: WidenedRangeSnapshotGone}
	}

	span := wholeSpan(*to)
	if snapshots.FromSnapshotID == "" {
		return span, nil
	}

	from := findSnapshot(params.Snapshots, snapshots.FromSnapshotID)
	switch {
	case from == nil:
		return ResolvedSpan{}, &Widening{Reason: WidenedRangeSnapshotGone}
	case from.Ordinal >= to.Ordinal:
		return ResolvedSpan{}, &Widening{Reason: WidenedRangeBackwards}
	}

	compared := compareSnapshots(ctx, compareSnapshotsParams{
		Git:      params.Git,
		Dir:      params.Dir,
		FromSHA:  from.SHA,
		FromBase: from.BaseSHA,
		ToBase:   to.BaseSHA,
	})
	span.BaseSHA, span.Replayed, span.Conflicts = compared.FromSHA, compared.Replayed, compared.Conflicts

	return span, nil
}

func findAnswerBase(snapshots []Snapshot, comment, answer *Snapshot) *Snapshot {
	base := comment
	for i := range snapshots {
		snapshot := &snapshots[i]
		if snapshot.Ordinal <= base.Ordinal || snapshot.Ordinal >= answer.Ordinal {
			continue
		}

		if snapshot.SHA == answer.SHA {
			continue
		}

		base = snapshot
	}

	return base
}

func findSnapshot(snapshots []Snapshot, id string) *Snapshot {
	for i := range snapshots {
		if snapshots[i].ID == id {
			return &snapshots[i]
		}
	}

	return nil
}

// findPreviousReviewedSnapshot finds the snapshot the person last commented on, or nothing when
// they never commented on this agent's work or its base has moved since.
func findPreviousReviewedSnapshot(comments []Comment, snapshots []Snapshot, snapshot Snapshot) (*Snapshot, *time.Time) {
	last := findLastComment(comments, snapshot.Ordinal)
	if last == nil {
		return nil, nil
	}

	earlier := findSnapshot(snapshots, last.SnapshotID)
	if earlier == nil || earlier.BaseSHA != snapshot.BaseSHA {
		return nil, &last.ReleasedAt
	}

	return earlier, &last.ReleasedAt
}

// findLastComment finds the person's newest comment on the latest snapshot before the one on
// screen.
func findLastComment(comments []Comment, ordinal int32) *Comment {
	var last *Comment
	for i := range comments {
		comment := &comments[i]
		if comment.SnapshotOrdinal >= ordinal {
			continue
		}
		if last == nil || comment.SnapshotOrdinal > last.SnapshotOrdinal ||
			comment.SnapshotOrdinal == last.SnapshotOrdinal && comment.ReleasedAt.After(last.ReleasedAt) {
			last = comment
		}
	}

	return last
}

type countChangedSinceParams struct {
	Git     Git
	Dir     string
	Exclude []string
	Earlier *Snapshot
	ToSHA   string
}

// countChangedSince counts the files changed since the person last released a review, or zero
// when git cannot say.
func countChangedSince(ctx context.Context, params countChangedSinceParams) int {
	if params.Earlier == nil {
		return 0
	}

	changed, err := params.Git.CountChangedFiles(ctx, git.CountChangedFilesParams{
		Dir:     params.Dir,
		FromSHA: params.Earlier.SHA,
		ToSHA:   params.ToSHA,
		Exclude: params.Exclude,
	})
	if err != nil {
		return 0
	}

	return changed
}

type snapshotComparison struct {
	FromSHA    string
	Comparable bool
	Rewritten  bool
	Replayed   bool
	Conflicts  []string
}

type compareSnapshotsParams struct {
	Git      Git
	Dir      string
	FromSHA  string
	FromBase string
	ToBase   string
}

// compareSnapshots works out the older side of a diff between two snapshots, carrying it onto the newer
// base when a rebase lies between them.
func compareSnapshots(ctx context.Context, params compareSnapshotsParams) snapshotComparison {
	if params.FromBase == params.ToBase {
		return snapshotComparison{
			FromSHA:    params.FromSHA,
			Comparable: true,
		}
	}

	reachable, err := params.Git.IsAncestor(ctx, git.IsAncestorParams{
		Dir:           params.Dir,
		MaybeAncestor: params.FromBase,
		Of:            params.ToBase,
	})
	compared := snapshotComparison{
		FromSHA:   params.FromSHA,
		Rewritten: err == nil && !reachable,
	}

	// Replay fails on a git too old to replay or a missing commit, and then the snapshots stay uncompared.
	replayed, err := params.Git.Replay(ctx, git.ReplayParams{
		Dir:         params.Dir,
		FromBase:    params.FromBase,
		OntoBase:    params.ToBase,
		SnapshotSHA: params.FromSHA,
	})
	if err != nil {
		return compared
	}

	if len(replayed.Conflicts) > 0 {
		compared.Conflicts = replayed.Conflicts

		return compared
	}

	compared.FromSHA = replayed.Tree
	compared.Comparable = true
	compared.Replayed = true

	return compared
}

type traceThreadsParams struct {
	Tracer    *Tracer
	Dir       string
	ToSHA     string
	Threads   []Thread
	Snapshots []Snapshot
}

// traceThreads asks git where each thread's lines went between the snapshot it was written
// against and the one on screen, giving up on every pair when one cannot be read.
func traceThreads(ctx context.Context, params traceThreadsParams) map[string]Place {
	if params.ToSHA == "" {
		return nil
	}

	exact := placeOnScreen(params)

	byFrom := groupBySnapshot(params)
	if len(byFrom) == 0 {
		return exact
	}

	ctx, cancel := context.WithTimeout(ctx, traceTimeout)
	defer cancel()

	traced := maps.Clone(exact)
	for from, threads := range byFrom {
		found, err := params.Tracer.TracePair(ctx, TracePairParams{
			Dir:     params.Dir,
			FromSHA: from,
			ToSHA:   params.ToSHA,
			Anchors: xslice.Map(threads, anchorOfThread),
		})
		if err != nil {
			return exact
		}

		maps.Copy(traced, found)
	}

	return traced
}

// placeOnScreen places each thread written against the snapshot on screen at its own lines.
func placeOnScreen(params traceThreadsParams) map[string]Place {
	onScreen := map[string]bool{}
	for _, snapshot := range params.Snapshots {
		if snapshot.SHA == params.ToSHA {
			onScreen[snapshot.ID] = true
		}
	}

	placed := map[string]Place{}
	for _, thread := range params.Threads {
		if !onScreen[thread.SnapshotID] || thread.NewStart <= 0 {
			continue
		}

		placed[thread.ID] = Place{
			FilePath: thread.FilePath,
			NewStart: thread.NewStart,
			Exact:    true,
		}
	}

	return placed
}

// groupBySnapshot gathers the threads worth tracing under the snapshot each was written against,
// leaving out the ones the screen will not render.
func groupBySnapshot(params traceThreadsParams) map[string][]Thread {
	snapshots := map[string]string{}
	ordinals := map[string]int32{}
	ceiling := int32(0)

	for _, snapshot := range params.Snapshots {
		snapshots[snapshot.ID] = snapshot.SHA
		ordinals[snapshot.ID] = snapshot.Ordinal

		if snapshot.SHA == params.ToSHA {
			ceiling = snapshot.Ordinal
		}
	}

	byFrom := map[string][]Thread{}
	for _, thread := range params.Threads {
		from := snapshots[thread.SnapshotID]

		// A thread on the snapshot on screen keeps its own lines, and a thread with no known snapshot has nothing to trace from.
		if from == "" || from == params.ToSHA {
			continue
		}

		// The screen never renders a thread from a later snapshot, and tracing one would diff the whole tree backwards for nothing.
		if ceiling > 0 && ordinals[thread.SnapshotID] > ceiling {
			continue
		}

		byFrom[from] = append(byFrom[from], thread)
	}

	return byFrom
}

func anchorOfThread(thread Thread) Anchor {
	return Anchor{
		ID: thread.ID,
		Position: position.Position{
			Path:  thread.FilePath,
			Lines: threadRange(thread),
		},
	}
}

// threadRange is a thread's new-side lines as a half-open span, and nothing for a thread on a
// line the newer side never held.
func threadRange(thread Thread) position.Range {
	if thread.NewStart <= 0 {
		return position.Range{}
	}

	end := max(thread.NewEnd, thread.NewStart)

	return position.Range{Start: int(thread.NewStart), End: int(end) + 1}
}
