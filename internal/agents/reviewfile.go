package agents

import (
	"fmt"
	"strconv"
	"strings"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
)

// contextLines is how much of the file either side of a comment goes into the review, which is
// enough to see what the comment is about without opening the file.
const contextLines = 6

// renderReviewParams is everything one review file has to say for itself, a released review
// outliving the rows that name it.
type renderReviewParams struct {
	// Agent is the agent whose work was reviewed.
	Agent agentstore.Agent
	Round reviewstore.Round
	// Author names who gave the review, which is a person when it is empty.
	Author  string
	Verdict reviewstore.Verdict
	// Said is what the author wrote about the work as a whole rather than about any one line,
	// empty when they wrote nothing, and it goes above the comments since it frames them.
	Said    string
	Threads []ReviewThreadWithMessages
	// Source holds each file as the round froze it, and a path it has nothing for falls back to
	// the lines the comment quoted.
	Source map[string][]string
}

// renderReview writes a whole review out as the markdown the agent reads, carrying everything it
// needs since the mail says only where this is, and drawing the code around each comment out of
// the round's own source.
func renderReview(params renderReviewParams) string {
	round, verdict, threads, source := params.Round, params.Verdict, params.Threads, params.Source

	var out strings.Builder

	fmt.Fprintf(&out, "# Review of round %d\n\n", round.Ordinal)
	fmt.Fprintf(&out, "%s\n\n", verdictSentence(verdict))
	fmt.Fprintf(&out, "On the work of **%s** (`%s`), reviewed by %s.\n\n",
		params.Agent.Name, params.Agent.ID, reviewerName(params.Author))
	fmt.Fprintf(&out, "This review was written against the snapshot `%s`, taken from `%s`. "+
		"To see exactly what was reviewed, run `git diff %s %s`.\n",
		short(round.SnapshotSHA), short(round.BaseSHA), round.BaseSHA, round.SnapshotSHA)

	if said := strings.TrimSpace(params.Said); said != "" {
		fmt.Fprintf(&out, "\n## What the reviewer said\n\n%s\n", said)
	}

	if len(threads) == 0 {
		return out.String()
	}

	out.WriteString("\n")
	for _, thread := range threads {
		writeThread(&out, thread, source[thread.Thread.FilePath], params.Author == "")
	}

	out.WriteString("Answer by writing in each thread, naming its id.\n")

	return out.String()
}

func writeThread(out *strings.Builder, thread ReviewThreadWithMessages, lines []string, byHuman bool) {
	fmt.Fprintf(out, "## %s\n\n", anchorOf(thread.Thread))
	fmt.Fprintf(out, "Thread `%s`\n\n", thread.Thread.ID)

	if !writeContext(out, thread.Thread, lines) && len(thread.Thread.QuotedLines) > 0 {
		out.WriteString("```diff\n")
		for _, line := range thread.Thread.QuotedLines {
			out.WriteString(line)
			out.WriteString("\n")
		}
		out.WriteString("```\n\n")
	}

	for _, message := range thread.Messages {
		out.WriteString(strings.TrimSpace(message.Body))
		out.WriteString("\n\n")

		if len(message.Suggestion) > 0 {
			out.WriteString(suggestionLead(byHuman))
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

// writeContext draws the file around the lines a comment was written about as the round froze
// them, marking those lines, and reports whether it drew anything at all.
func writeContext(out *strings.Builder, thread reviewstore.Thread, lines []string) bool {
	start, end := int(thread.NewStart), int(thread.NewEnd)
	if start <= 0 || start > len(lines) {
		return false
	}
	if end < start {
		end = start
	}

	from, to := max(start-contextLines, 1), min(end+contextLines, len(lines))
	width := len(strconv.Itoa(to))

	out.WriteString("```\n")
	for number := from; number <= to; number++ {
		mark := "  "
		if number >= start && number <= end {
			mark = ">>"
		}

		fmt.Fprintf(out, "%s %*d  %s\n", mark, width, number, lines[number-1])
	}
	out.WriteString("```\n\n")

	return true
}

// anchorOf names where a thread is pinned in the terms the diff uses, preferring the new side
// since that is where the code the comment is about ended up.
func anchorOf(thread reviewstore.Thread) string {
	start, end := thread.NewStart, thread.NewEnd
	if start == 0 {
		start, end = thread.OldStart, thread.OldEnd
	}

	if end > start {
		return fmt.Sprintf("%s:%d-%d", thread.FilePath, start, end)
	}

	return fmt.Sprintf("%s:%d", thread.FilePath, start)
}

// reviewerName is who gave the review, spelled for somebody reading the file long afterwards,
// where an empty author is the person running weave rather than an agent.
func reviewerName(author string) string {
	if author == "" {
		return "the person running weave"
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

func short(sha string) string {
	if len(sha) <= 12 {
		return sha
	}

	return sha[:12]
}
