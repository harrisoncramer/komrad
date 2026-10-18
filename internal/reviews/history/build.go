package history

import (
	"context"
	"maps"
	"strings"
	"time"

	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/position"
	"komrad/lib/xslice"
)

// traceTimeout caps tracing every thread of one history together.
const traceTimeout = 10 * time.Second

var errAnswerGone = fail.NotFound("the rounds this answer stands between are gone")

var errCommentRoundGone = fail.NotFound("the round this comment was written on is gone")

var errRangeRoundGone = fail.NotFound("a round this range stands between is gone")

var errRangeBackwards = fail.InvalidArgument("the range starts on or after the round it ends on")

type Git interface {
	CountChangedFiles(ctx context.Context, params git.CountChangedFilesParams) (int, error)
	Replay(ctx context.Context, params git.ReplayParams) (*git.Replayed, error)
	IsAncestor(ctx context.Context, params git.IsAncestorParams) (bool, error)
}

type Round struct {
	ID          string
	Ordinal     int32
	BaseSHA     string
	SnapshotSHA string
}

type Review struct {
	RoundID      string
	RoundOrdinal int32
	Human        bool
	ReleasedAt   *time.Time
}

type Thread struct {
	ID       string
	RoundID  string
	FilePath string
	NewStart int32
	NewEnd   int32
}

type Span struct {
	Rounds *RoundSpan
	Answer *AnswerSpan
	Origin *OriginSpan
}

type AnswerSpan struct {
	FromRoundID string
	ToRoundID   string
}

type RoundSpan struct {
	FromRoundID string
	ToRoundID   string
}

type OriginSpan struct {
	RoundID string
	BaseSHA string
}

type ResolvedSpan struct {
	BaseSHA            string
	HeadSHA            string
	Replayed           bool
	AnswerBaseSHA      string
	AnswerHeadSHA      string
	OriginBaseSHA      string
	OriginHeadSHA      string
	OriginRoundBaseSHA string
	Conflicts          []string
}

type Record struct {
	Span                ResolvedSpan
	Widened             string
	Earlier             *Round
	EarlierReleasedAt   *time.Time
	ChangedSinceEarlier int
	Traced              map[string]Place
}

type BuildParams struct {
	Git     Git
	Tracer  *Tracer
	Dir     string
	Exclude []string
	Round   Round
	Rounds  []Round
	Reviews []Review
	Threads []Thread
	Span    Span
}

// Build measures the span asked for against the round on screen and traces where each thread's lines now sit.
func Build(ctx context.Context, params BuildParams) Record {
	span, err := resolveSpan(ctx, params)

	record := Record{Span: span}
	if err != nil {
		record.Widened = err.Error()
		record.Span = wholeSpan(params.Round)
	}

	record.Earlier, record.EarlierReleasedAt = findPreviousReviewedRound(params.Reviews, params.Rounds, params.Round)
	record.ChangedSinceEarlier = countChangedSince(ctx, countChangedSinceParams{
		Git:     params.Git,
		Dir:     params.Dir,
		Exclude: params.Exclude,
		Earlier: record.Earlier,
		ToSHA:   params.Round.SnapshotSHA,
	})

	record.Traced = traceThreads(ctx, traceThreadsParams{
		Tracer:  params.Tracer,
		Dir:     params.Dir,
		ToSHA:   record.Span.HeadSHA,
		Threads: params.Threads,
		Rounds:  params.Rounds,
	})

	return record
}

func wholeSpan(round Round) ResolvedSpan {
	return ResolvedSpan{BaseSHA: round.BaseSHA, HeadSHA: round.SnapshotSHA}
}

type roundsApartError struct {
	Conflicts []string
}

func (e *roundsApartError) Error() string {
	said := "the answer can't be compared across the rebase"
	if len(e.Conflicts) > 0 {
		said += ", conflicts in " + strings.Join(e.Conflicts, ", ")
	}

	return said
}

func resolveSpan(ctx context.Context, params BuildParams) (ResolvedSpan, error) {
	asked := params.Span
	if rounds := asked.Rounds; rounds != nil {
		return resolveRounds(ctx, params, *rounds)
	}

	span := wholeSpan(params.Round)

	var fromBase, toBase string
	if answer := asked.Answer; answer != nil {
		from, to := findRound(params.Rounds, answer.FromRoundID), findRound(params.Rounds, answer.ToRoundID)
		if from == nil || to == nil {
			return ResolvedSpan{}, errAnswerGone
		}

		from = findAnswerBase(params.Rounds, from, to)

		span.AnswerBaseSHA, span.AnswerHeadSHA = from.SnapshotSHA, to.SnapshotSHA
		span.BaseSHA, span.HeadSHA = span.AnswerBaseSHA, span.AnswerHeadSHA
		fromBase, toBase = from.BaseSHA, to.BaseSHA
	}

	if origin := asked.Origin; origin != nil {
		round := findRound(params.Rounds, origin.RoundID)
		if round == nil {
			return ResolvedSpan{}, errCommentRoundGone
		}

		span.OriginBaseSHA, span.OriginHeadSHA, span.OriginRoundBaseSHA = origin.BaseSHA, round.SnapshotSHA, round.BaseSHA
		if span.OriginBaseSHA == "" {
			span.OriginBaseSHA = round.BaseSHA
		}
		span.BaseSHA, span.HeadSHA = span.OriginBaseSHA, span.OriginHeadSHA
	}

	compared := compareRounds(ctx, compareRoundsParams{
		Git:      params.Git,
		Dir:      params.Dir,
		FromSHA:  span.BaseSHA,
		FromBase: fromBase,
		ToBase:   toBase,
	})
	if !compared.Comparable {
		return ResolvedSpan{}, &roundsApartError{Conflicts: compared.Conflicts}
	}

	span.BaseSHA, span.Replayed = compared.FromSHA, compared.Replayed

	return span, nil
}

// resolveRounds measures the work between two rounds, from the older one as it was when its
// replay onto the newer base conflicts.
func resolveRounds(ctx context.Context, params BuildParams, rounds RoundSpan) (ResolvedSpan, error) {
	to := &params.Round
	if rounds.ToRoundID != "" {
		to = findRound(params.Rounds, rounds.ToRoundID)
	}
	if to == nil {
		return ResolvedSpan{}, errRangeRoundGone
	}

	span := wholeSpan(*to)
	if rounds.FromRoundID == "" {
		return span, nil
	}

	from := findRound(params.Rounds, rounds.FromRoundID)
	switch {
	case from == nil:
		return ResolvedSpan{}, errRangeRoundGone
	case from.Ordinal >= to.Ordinal:
		return ResolvedSpan{}, errRangeBackwards
	}

	compared := compareRounds(ctx, compareRoundsParams{
		Git:      params.Git,
		Dir:      params.Dir,
		FromSHA:  from.SnapshotSHA,
		FromBase: from.BaseSHA,
		ToBase:   to.BaseSHA,
	})
	span.BaseSHA, span.Replayed, span.Conflicts = compared.FromSHA, compared.Replayed, compared.Conflicts

	return span, nil
}

func findAnswerBase(rounds []Round, comment, answer *Round) *Round {
	base := comment
	for i := range rounds {
		round := &rounds[i]
		if round.Ordinal <= base.Ordinal || round.Ordinal >= answer.Ordinal {
			continue
		}

		if round.SnapshotSHA == answer.SnapshotSHA {
			continue
		}

		base = round
	}

	return base
}

func findRound(rounds []Round, id string) *Round {
	for i := range rounds {
		if rounds[i].ID == id {
			return &rounds[i]
		}
	}

	return nil
}

// findPreviousReviewedRound finds the round the person last released a review against, or nothing
// when they never reviewed this agent or its base has moved since.
func findPreviousReviewedRound(reviews []Review, rounds []Round, round Round) (*Round, *time.Time) {
	last := findLastHumanReview(reviews, round.Ordinal)
	if last == nil {
		return nil, nil
	}

	earlier := findRound(rounds, last.RoundID)
	if earlier == nil || earlier.BaseSHA != round.BaseSHA {
		return nil, last.ReleasedAt
	}

	return earlier, last.ReleasedAt
}

// findLastHumanReview finds the newest review the person released against a round before the one
// on screen.
func findLastHumanReview(reviews []Review, ordinal int32) *Review {
	var last *Review
	for i := range reviews {
		review := &reviews[i]
		if !review.Human || review.ReleasedAt == nil || review.RoundOrdinal >= ordinal {
			continue
		}

		last = review
	}

	return last
}

type countChangedSinceParams struct {
	Git     Git
	Dir     string
	Exclude []string
	Earlier *Round
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
		FromSHA: params.Earlier.SnapshotSHA,
		ToSHA:   params.ToSHA,
		Exclude: params.Exclude,
	})
	if err != nil {
		return 0
	}

	return changed
}

type roundComparison struct {
	FromSHA    string
	Comparable bool
	Rewritten  bool
	Replayed   bool
	Conflicts  []string
}

type compareRoundsParams struct {
	Git      Git
	Dir      string
	FromSHA  string
	FromBase string
	ToBase   string
}

// compareRounds works out the older side of a diff between two rounds, carrying it onto the newer
// base when a rebase lies between them.
func compareRounds(ctx context.Context, params compareRoundsParams) roundComparison {
	if params.FromBase == params.ToBase {
		return roundComparison{
			FromSHA:    params.FromSHA,
			Comparable: true,
		}
	}

	reachable, err := params.Git.IsAncestor(ctx, git.IsAncestorParams{
		Dir:           params.Dir,
		MaybeAncestor: params.FromBase,
		Of:            params.ToBase,
	})
	compared := roundComparison{
		FromSHA:   params.FromSHA,
		Rewritten: err == nil && !reachable,
	}

	// Replay fails on a git too old to replay or a missing commit, and then the rounds stay uncompared.
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
	Tracer  *Tracer
	Dir     string
	ToSHA   string
	Threads []Thread
	Rounds  []Round
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
	for _, round := range params.Rounds {
		if round.SnapshotSHA == params.ToSHA {
			onScreen[round.ID] = true
		}
	}

	placed := map[string]Place{}
	for _, thread := range params.Threads {
		if !onScreen[thread.RoundID] || thread.NewStart <= 0 {
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

	for _, round := range params.Rounds {
		snapshots[round.ID] = round.SnapshotSHA
		ordinals[round.ID] = round.Ordinal

		if round.SnapshotSHA == params.ToSHA {
			ceiling = round.Ordinal
		}
	}

	byFrom := map[string][]Thread{}
	for _, thread := range params.Threads {
		from := snapshots[thread.RoundID]

		// A thread on the snapshot on screen keeps its own lines, and a thread with no known round has nothing to trace from.
		if from == "" || from == params.ToSHA {
			continue
		}

		// The screen never renders a thread from a later round, and tracing one would diff the whole tree backwards for nothing.
		if ceiling > 0 && ordinals[thread.RoundID] > ceiling {
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
