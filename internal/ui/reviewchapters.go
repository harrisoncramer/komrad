package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"weave/internal/git"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

// reviewLegendFloor is the shortest pane the expanded legend fits in once it carries the base
// row.
const reviewLegendFloor = 21

// reviewChapter is one round of the review as a reader reads it: the work between two of their
// reviews, or between their last one and now.
type reviewChapter struct {
	// FromSHA is the snapshot the chapter starts after, empty for the point the branch left main.
	FromSHA string
	// ToSHA is the snapshot the chapter ends at.
	ToSHA string
	// At is when the review closing the chapter was released, in clock terms, empty for the
	// chapter nobody has reviewed yet.
	At string
	// Ordinal is the round the chapter ends at, which is the newest round whose comments existed
	// while it was the work in hand.
	Ordinal int32
	// Until is when the agent moved past this chapter, in RFC 3339, empty for the chapter nothing
	// follows.
	Until string
	// Comparable is false for a chapter whose two ends hang off different base commits, which a
	// rebase between two reviews leaves behind.
	Comparable bool
	// FromBase is the commit the chapter's start was measured from.
	FromBase string
	// ToBase is the commit its end was measured from.
	ToBase string
	// Rewritten says the base the chapter starts on is not reachable from the one it ends on.
	Rewritten bool
	// Replayed says the start was carried onto the end's base.
	Replayed bool
	// Conflicts is the paths the base rewrote under the agent, which a replay could not settle.
	Conflicts []string
}

// stepReviewBase walks back a round, from the whole change to the newest round and on into the
// older ones, and stops on the oldest.
func (m model) stepReviewBase() (tea.Model, tea.Cmd) {
	if m.review.chapter < 0 {
		return m.showReviewChapter(len(m.review.chapters) - 1)
	}

	return m.showReviewChapter(m.review.chapter - 1)
}

// stepReviewBaseForward walks the other way, and stops on the whole change.
func (m model) stepReviewBaseForward() (tea.Model, tea.Cmd) {
	if m.review.chapter < 0 {
		return m, nil
	}

	return m.showReviewChapter(m.review.chapter + 1)
}

// showReviewChapter puts one round of the review on screen, or the whole change past the newest
// round, stopping at either end so a held key walks to the end and stays there.
func (m model) showReviewChapter(at int) (tea.Model, tea.Cmd) {
	held := len(m.review.chapters)
	if held == 0 {
		return m, nil
	}

	// The whole change sits after the newest round rather than before the oldest, since it is
	// the view that carries every round at once.
	switch {
	case at < 0:
		at = 0
	case at >= held:
		at = -1
	}

	if at == m.review.chapter {
		return m, nil
	}

	m.review.chapter = at
	if at < 0 {
		m.widenReviewBase()

		next, cmd := m.reloadReview()

		return next, cmd
	}

	chapter := m.review.chapters[at]
	m.review.base = chapter.FromSHA
	m.review.head = chapter.ToSHA
	m.review.baseLabel = reviewChapterLabel(reviewChapterLabelParams{
		Chapter: chapter,
		At:      at,
		Held:    held,
	})

	next, cmd := m.reloadReview()

	return next, cmd
}

// reviewChapterLabelParams is one chapter and where it sits in the walk.
type reviewChapterLabelParams struct {
	Chapter reviewChapter
	At      int
	Held    int
}

// reviewChapterLabel names a chapter for the band, counting from the oldest so a number keeps
// meaning the same round however many follow it.
func reviewChapterLabel(params reviewChapterLabelParams) string {
	said := fmt.Sprintf("round %d of %d", params.At+1, params.Held)
	chapter := params.Chapter

	switch {
	case !chapter.Comparable:
		said += ", not comparable, " + baseMovedSaid(chapter)
		if len(chapter.Conflicts) > 0 {
			said += ", conflicts in " + strings.Join(chapter.Conflicts, ", ")
		}

		return said
	case chapter.At == "":
		said += ", not reviewed yet"
	default:
		said += ", reviewed at " + chapter.At
	}

	if chapter.Replayed {
		said += ", " + baseMovedSaid(chapter) + ", replayed"
	}

	return said
}

// baseMovedSaid names which of the two things happened to the base between a chapter's ends.
func baseMovedSaid(chapter reviewChapter) string {
	if chapter.Rewritten {
		return "history was rewritten"
	}

	return "the base moved"
}

// widenReviewBase puts the diff back to the whole change.
func (m *model) widenReviewBase() {
	m.review.base = ""
	m.review.head = ""
	m.review.baseLabel = ""
	m.review.chapter = -1
}

// doesReviewFitBaseLegend reports whether the base key is worth a legend row.
func (m model) doesReviewFitBaseLegend() bool {
	if m.helpExpanded && m.height > 0 && m.height < reviewLegendFloor {
		return false
	}

	return m.doesReviewWalk()
}

// reviewBaseDesc names the base key in the legend, inside its own width budget.
func (m model) reviewBaseDesc() string {
	return "an earlier round"
}

// reviewBaseForwardDesc is the same for the key that walks the other way.
func (m model) reviewBaseForwardDesc() string {
	return "a later round"
}

// cutChaptersParams is everything the chapters are cut from, kept apart from the calls that read
// it so the cutting can be tested on its own.
type cutChaptersParams struct {
	Rounds   []*agentv1.ReviewRound
	Reviews  []*agentv1.Review
	OnScreen *agentv1.ReviewRound
}

// cutChapters cuts the rounds at every round the person released a review against, and ends the
// last chapter at the round on screen.
func cutChapters(params cutChaptersParams) []reviewChapter {
	closed := map[int32]string{}
	for _, review := range params.Reviews {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if released := review.GetReleasedAt(); released != "" {
			closed[review.GetRoundOrdinal()] = releasedClock(released)
		}
	}

	milestones := make([]*agentv1.ReviewRound, 0, len(closed)+1)
	for _, candidate := range params.Rounds {
		if candidate.GetOrdinal() > params.OnScreen.GetOrdinal() {
			continue
		}
		if _, ok := closed[candidate.GetOrdinal()]; ok {
			milestones = append(milestones, candidate)
		}
	}

	// The work nobody has reviewed yet is a chapter of its own, and it is the one a reader
	// arrives wanting, so it is only left out when the newest review closed the round on screen.
	if _, ok := closed[params.OnScreen.GetOrdinal()]; !ok {
		milestones = append(milestones, params.OnScreen)
	}

	if len(milestones) < 2 {
		return nil
	}

	chapters := make([]reviewChapter, 0, len(milestones))
	from := ""
	fromBase := params.OnScreen.GetBaseSha()
	for _, milestone := range milestones {
		chapters = append(chapters, reviewChapter{
			FromSHA: from,
			ToSHA:   milestone.GetSnapshotSha(),
			At:      closed[milestone.GetOrdinal()],
			Ordinal: milestone.GetOrdinal(),
			Until: roundAfter(roundAfterParams{
				Rounds:  params.Rounds,
				After:   milestone.GetOrdinal(),
				Ceiling: params.OnScreen.GetOrdinal(),
			}),
			Comparable: from == "" || fromBase == milestone.GetBaseSha(),
			FromBase:   fromBase,
			ToBase:     milestone.GetBaseSha(),
		})

		from, fromBase = milestone.GetSnapshotSha(), milestone.GetBaseSha()
	}

	return chapters
}

// replayChaptersParams is the chapters as cut and where to read the repository.
type replayChaptersParams struct {
	Git      uiGit
	Dir      string
	Chapters []reviewChapter
}

// replayChapters carries the start of every chapter whose base moved onto the base its end
// stands on.
func replayChapters(ctx context.Context, params replayChaptersParams) []reviewChapter {
	for i := range params.Chapters {
		chapter := &params.Chapters[i]
		if chapter.Comparable {
			continue
		}

		chapter.Rewritten = isBaseRewritten(ctx, params, *chapter)

		// A git too old to replay, or a commit gone, leaves the refusal standing.
		replayed, err := params.Git.Replay(ctx, git.ReplayParams{
			Dir:         params.Dir,
			FromBase:    chapter.FromBase,
			OntoBase:    chapter.ToBase,
			SnapshotSHA: chapter.FromSHA,
		})
		if err != nil {
			continue
		}

		if len(replayed.Conflicts) > 0 {
			chapter.Conflicts = replayed.Conflicts

			continue
		}

		chapter.FromSHA = replayed.Tree
		chapter.Comparable = true
		chapter.Replayed = true
	}

	return params.Chapters
}

// isBaseRewritten reports whether a chapter's start sits on a base its end cannot reach, and
// says no when git cannot answer.
func isBaseRewritten(ctx context.Context, params replayChaptersParams, chapter reviewChapter) bool {
	reachable, err := params.Git.IsAncestor(ctx, params.Dir, chapter.FromBase, chapter.ToBase)

	return err == nil && !reachable
}

// threadsByChapterParams is the whole conversation and which round of it is on screen.
type threadsByChapterParams struct {
	Threads  []*agentv1.ReviewThread
	Chapters []reviewChapter
	At       int
}

// threadsByChapter drops the comments written after the round on screen, and gives back the
// whole conversation while the whole change is on screen.
func threadsByChapter(params threadsByChapterParams) []*agentv1.ReviewThread {
	if params.At < 0 || params.At >= len(params.Chapters) {
		return params.Threads
	}

	chapter := params.Chapters[params.At]

	kept := make([]*agentv1.ReviewThread, 0, len(params.Threads))
	for _, thread := range params.Threads {
		if thread.GetRoundOrdinal() > chapter.Ordinal {
			continue
		}

		kept = append(kept, messagesByChapter(thread, chapter.Until))
	}

	return kept
}

// messagesByChapter is one thread with the replies written after the chapter closed left out, so
// a conversation that carried on about later code is read as it stood then.
func messagesByChapter(thread *agentv1.ReviewThread, until string) *agentv1.ReviewThread {
	if until == "" {
		return thread
	}

	said := thread.GetMessages()

	kept := make([]*agentv1.ReviewMessage, 0, len(said))
	for _, message := range said {
		if doesPredate(message.GetCreatedAt(), until) {
			kept = append(kept, message)
		}
	}

	if len(kept) == len(said) {
		return thread
	}

	// The threads belong to the model rather than to the view, so the shortened one is a copy.
	trimmed, ok := proto.Clone(thread).(*agentv1.ReviewThread)
	if !ok {
		return thread
	}

	trimmed.Messages = kept

	return trimmed
}

// isBefore reports whether one stamp came before another, parsed rather than compared as text.
func isBefore(stamp, mark string) bool {
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return false
	}

	against, err := time.Parse(time.RFC3339, mark)
	if err != nil {
		return false
	}

	return at.Before(against)
}

// doesPredate reports whether something was said before a mark, and says yes for a stamp it
// cannot read.
func doesPredate(stamp, mark string) bool {
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return true
	}

	against, err := time.Parse(time.RFC3339, mark)
	if err != nil {
		return true
	}

	return at.Before(against)
}

// roundAfterParams says which round to look past and how far the reader can see.
type roundAfterParams struct {
	Rounds []*agentv1.ReviewRound
	// After is the round the chapter ends at.
	After int32
	// Ceiling is the round on screen, past which a round is not something the reader is reading.
	Ceiling int32
}

// roundAfter is when the round following an ordinal was taken, and nothing when the reader is
// standing on the last of them.
func roundAfter(params roundAfterParams) string {
	after := ""
	for _, round := range params.Rounds {
		if round.GetOrdinal() <= params.After || round.GetOrdinal() > params.Ceiling {
			continue
		}
		if after == "" || isBefore(round.GetCreatedAt(), after) {
			after = round.GetCreatedAt()
		}
	}

	return after
}
