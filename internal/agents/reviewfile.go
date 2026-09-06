package agents

import (
	"fmt"
	"strconv"
	"strings"

	reviewstore "weave/internal/store/review"
)

// contextLines is how much of the file either side of a comment goes into the review, which is
// enough to see what the comment is about without opening the file.
const contextLines = 6

// RenderReview writes a whole review out as the markdown the agent reads, carrying everything it
// needs since the mail says only where this is, and drawing the code around each comment out of
// source, which holds each file as the round froze it and falls back to the quoted lines for a
// path it has nothing for.
func RenderReview(
	round reviewstore.Round,
	verdict reviewstore.Verdict,
	threads []ReviewThreadWithMessages,
	source map[string][]string,
) string {
	var out strings.Builder

	fmt.Fprintf(&out, "# Review of round %d\n\n", round.Ordinal)
	fmt.Fprintf(&out, "%s\n\n", verdictSentence(verdict))
	fmt.Fprintf(&out, "This review was written against the snapshot `%s`, taken from `%s`. "+
		"To see exactly what was reviewed, run `git diff %s %s`.\n",
		short(round.SnapshotSHA), short(round.BaseSHA), round.BaseSHA, round.SnapshotSHA)

	if len(threads) == 0 {
		return out.String()
	}

	out.WriteString("\n")
	for _, thread := range threads {
		writeThread(&out, thread, source[thread.Thread.FilePath])
	}

	out.WriteString("Answer by writing in each thread, naming its id.\n")

	return out.String()
}

func writeThread(out *strings.Builder, thread ReviewThreadWithMessages, lines []string) {
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
			out.WriteString("Suggested replacement for those lines:\n\n```\n")
			for _, line := range message.Suggestion {
				out.WriteString(line)
				out.WriteString("\n")
			}
			out.WriteString("```\n\n")
		}
	}
}

// writeContext draws the file around the lines a comment was written about as the round froze
// them, marking those lines, and reports whether it drew anything at all, since a file the round
// does not hold falls back to the lines the comment was quoted against.
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

func verdictSentence(verdict reviewstore.Verdict) string {
	switch verdict {
	case reviewstore.VerdictApproved:
		return "Approved. This round is good as it stands."
	case reviewstore.VerdictChangesRequested:
		return "Changes requested. Keep working on this."
	case reviewstore.VerdictCommented:
		return "Comments only, with no conclusion either way."
	default:
		return "Comments only, with no conclusion either way."
	}
}

func short(sha string) string {
	if len(sha) <= 12 {
		return sha
	}

	return sha[:12]
}
