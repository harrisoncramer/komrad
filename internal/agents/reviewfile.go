package agents

import (
	"fmt"
	"strings"

	reviewstore "weave/internal/store/review"
)

// RenderReview writes a whole review out as the markdown the agent reads, which has to carry
// everything the agent needs, since the mail weave sends says only where this is.
func RenderReview(
	round reviewstore.Round,
	verdict reviewstore.Verdict,
	threads []ReviewThreadWithMessages,
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
		writeThread(&out, thread)
	}

	out.WriteString("Answer by writing in each thread, naming its id.\n")

	return out.String()
}

func writeThread(out *strings.Builder, thread ReviewThreadWithMessages) {
	fmt.Fprintf(out, "## %s\n\n", anchorOf(thread.Thread))
	fmt.Fprintf(out, "Thread `%s`\n\n", thread.Thread.ID)

	if len(thread.Thread.QuotedLines) > 0 {
		out.WriteString("```diff\n")
		for _, line := range thread.Thread.QuotedLines {
			out.WriteString(line)
			out.WriteString("\n")
		}
		out.WriteString("```\n\n")
	}

	for _, message := range thread.Messages {
		if message.AuthorKind == reviewstore.AuthorAgent {
			continue
		}

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
