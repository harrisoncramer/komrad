package reviews

import (
	"fmt"
	"strconv"
	"strings"

	"komrade/internal/git"
	agentstore "komrade/internal/store/agent"
	reviewstore "komrade/internal/store/review"
)

// contextLines is how much of the file either side of a comment goes into the review.
const contextLines = 6

// renderReviewParams is everything one review file has to say for itself.
type renderReviewParams struct {
	// Agent is the agent whose work was reviewed.
	Agent agentstore.Agent
	Round reviewstore.Round
	// Author names who gave the review, and is empty when a person gave it.
	Author  string
	Verdict reviewstore.Verdict
	// Said is what the reviewer wrote about the change as a whole rather than about any one line,
	// empty when they wrote nothing.
	Said    string
	Threads []ReviewThreadInContext
}

// renderReview writes a whole review out as the markdown the agent reads, drawing the code
// around each comment out of the round's own source.
func renderReview(params renderReviewParams) string {
	round, verdict, threads := params.Round, params.Verdict, params.Threads

	var out strings.Builder

	fmt.Fprintf(&out, "# Review of round %d\n\n", round.Ordinal)
	fmt.Fprintf(&out, "%s\n\n", verdictSentence(verdict))
	fmt.Fprintf(&out, "On the work of **%s** (`%s`), reviewed by %s.\n\n",
		params.Agent.Name, params.Agent.ID, reviewerName(params.Author))
	fmt.Fprintf(&out, "This review was written against the snapshot `%s`, taken from `%s`. "+
		"To see exactly what was reviewed, run `git diff %s %s`.\n",
		git.ShortSHA(round.SnapshotSHA), git.ShortSHA(round.BaseSHA), round.BaseSHA, round.SnapshotSHA)

	if said := strings.TrimSpace(params.Said); said != "" {
		fmt.Fprintf(&out, "\n## What the reviewer said\n\n%s\n", said)
	}

	if len(threads) == 0 {
		return out.String()
	}

	out.WriteString("\n")
	for _, thread := range threads {
		writeThread(&out, writeThreadParams{
			Thread:  thread,
			Round:   round,
			ByHuman: params.Author == "",
		})
	}

	out.WriteString("Answer by writing in each thread, naming its id.\n")

	return out.String()
}

type writeThreadParams struct {
	Thread  ReviewThreadInContext
	Round   reviewstore.Round
	ByHuman bool
}

func writeThread(out *strings.Builder, params writeThreadParams) {
	thread := params.Thread.Thread

	fmt.Fprintf(out, "## %s\n\n", anchorOf(thread))
	fmt.Fprintf(out, "Thread `%s`\n\n", thread.ID)

	if isOnOldSide(thread) {
		fmt.Fprintf(out, "%s\n\n", oldSideSentence(thread, params.Round))
	}

	if len(params.Thread.Context) > 0 {
		out.WriteString("```\n")
		for _, line := range params.Thread.Context {
			out.WriteString(line)
			out.WriteString("\n")
		}
		out.WriteString("```\n\n")
	} else if len(thread.QuotedLines) > 0 {
		out.WriteString("```diff\n")
		for _, line := range thread.QuotedLines {
			out.WriteString(line)
			out.WriteString("\n")
		}
		out.WriteString("```\n\n")
	}

	for _, message := range params.Thread.Messages {
		out.WriteString(strings.TrimSpace(message.Body))
		out.WriteString("\n\n")

		if len(message.Suggestion) > 0 {
			out.WriteString(suggestionLead(params.ByHuman))
			out.WriteString("\n\n```\n")
			for _, line := range message.Suggestion {
				out.WriteString(line)
				out.WriteString("\n")
			}
			out.WriteString("```\n\n")
		}
	}
}

// suggestionLead says how much weight the replacement lines carry.
func suggestionLead(byHuman bool) string {
	if byHuman {
		return "The person reviewing wrote this replacement for those lines themselves. " +
			"Apply it as written. If it no longer applies because the code was renamed or removed, " +
			"say so in the thread rather than half-applying it:"
	}

	return "Suggested replacement for those lines:"
}

// hasContext reports whether the round's file holds the lines a comment was written about.
func hasContext(thread reviewstore.Thread, lines []string) bool {
	start := int(thread.NewStart)

	return start > 0 && start <= len(lines)
}

// contextOf is the file around the lines a comment was written about as the round froze them,
// numbered, with those lines marked, and nothing for a comment the file does not reach.
func contextOf(thread reviewstore.Thread, lines []string) []string {
	if !hasContext(thread, lines) {
		return nil
	}

	start, end := int(thread.NewStart), int(thread.NewEnd)
	if end < start {
		end = start
	}

	from, to := max(start-contextLines, 1), min(end+contextLines, len(lines))
	width := len(strconv.Itoa(to))

	drawn := make([]string, 0, to-from+1)
	for number := from; number <= to; number++ {
		mark := "  "
		if number >= start && number <= end {
			mark = ">>"
		}

		drawn = append(drawn, fmt.Sprintf("%s %*d  %s", mark, width, number, lines[number-1]))
	}

	return drawn
}

// anchorOf names where a thread is pinned in the terms the diff uses, preferring the new side.
func anchorOf(thread reviewstore.Thread) string {
	start, end, side := thread.NewStart, thread.NewEnd, ""
	if isOnOldSide(thread) {
		start, end, side = thread.OldStart, thread.OldEnd, " (removed)"
	}

	if end > start {
		return fmt.Sprintf("%s:%d-%d%s", thread.FilePath, start, end, side)
	}

	return fmt.Sprintf("%s:%d%s", thread.FilePath, start, side)
}

func isOnOldSide(thread reviewstore.Thread) bool {
	return thread.NewStart == 0 && thread.OldStart > 0
}

func oldSideSentence(thread reviewstore.Thread, round reviewstore.Round) string {
	base := thread.BaseSHA
	if base == "" {
		base = round.BaseSHA
	}

	return fmt.Sprintf("This thread's line numbers are on the old side, counted in `%s` rather than "+
		"in the snapshot. To see those lines, run `git diff %s %s`.",
		git.ShortSHA(base), base, round.SnapshotSHA)
}

// reviewerName is who gave the review, spelled for somebody reading the file long afterwards,
// an empty author meaning the person running komrade.
func reviewerName(author string) string {
	if author == "" {
		return "the person running komrade"
	}

	return "the agent " + author
}

func verdictSentence(verdict reviewstore.Verdict) string {
	switch verdict {
	case reviewstore.VerdictApproved:
		return "Approved. This round is good as it stands."
	case reviewstore.VerdictChangesRequested:
		return "Changes requested. Keep working on this."
	case reviewstore.VerdictCommented:
	}

	return "Comments only, with no conclusion either way."
}
