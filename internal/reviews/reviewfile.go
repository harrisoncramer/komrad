package reviews

import (
	"fmt"
	"strconv"
	"strings"

	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xsha"
)

// contextLines is how much of the file either side of a comment goes into the review.
const contextLines = 6

// renderReviewParams is everything one review file has to say for itself.
type renderReviewParams struct {
	// Task is the task whose work was reviewed.
	Task     taskstore.Task
	Snapshot reviewstore.Snapshot
	// Author names who gave the review, and is empty when a person gave it.
	Author  string
	Verdict reviewstore.Verdict
	// Said is what the reviewer wrote about the change as a whole rather than about any one line,
	// empty when they wrote nothing.
	Said    string
	Threads []ReviewThreadInContext
}

// renderReview writes a whole review out as the markdown the agent reads, drawing the code
// around each comment out of the snapshot's own source.
func renderReview(params renderReviewParams) string {
	snapshot, verdict, threads := params.Snapshot, params.Verdict, params.Threads

	var out strings.Builder

	fmt.Fprintf(&out, "# Review of snapshot %d\n\n", snapshot.Ordinal)
	fmt.Fprintf(&out, "%s\n\n", verdictSentence(verdictSentenceParams{
		Verdict: verdict,
		Person:  params.Author == "",
	}))
	fmt.Fprintf(&out, "On the work of task **%s** (`%s`), reviewed by %s.\n\n",
		params.Task.Slug, params.Task.ID, reviewerName(params.Author))
	fmt.Fprintf(&out, "This review was written against the snapshot `%s`, taken from `%s`. "+
		"To see exactly what was reviewed, run `git diff %s %s`.\n",
		xsha.Short(snapshot.SHA), xsha.Short(snapshot.BaseSHA), snapshot.BaseSHA, snapshot.SHA)

	if said := strings.TrimSpace(params.Said); said != "" {
		fmt.Fprintf(&out, "\n## What the reviewer said\n\n%s\n", said)
	}

	if len(threads) == 0 {
		return out.String()
	}

	out.WriteString("\n")
	for _, thread := range threads {
		writeThread(&out, writeThreadParams{
			Thread:   thread,
			Snapshot: snapshot,
			ByHuman:  params.Author == "",
		})
	}

	out.WriteString("Answer by writing in each thread, naming its id.\n")

	return out.String()
}

type writeThreadParams struct {
	Thread   ReviewThreadInContext
	Snapshot reviewstore.Snapshot
	ByHuman  bool
}

func writeThread(out *strings.Builder, params writeThreadParams) {
	thread := params.Thread.Thread

	fmt.Fprintf(out, "## %s\n\n", anchorOf(thread))
	fmt.Fprintf(out, "Thread `%s`\n\n", thread.ID)

	if isOnOldSide(thread) {
		fmt.Fprintf(out, "%s\n\n", oldSideSentence(thread, params.Snapshot))
	}

	if len(params.Thread.Context) > 0 {
		writeFenced(out, "", params.Thread.Context)
	} else if len(thread.QuotedLines) > 0 {
		writeFenced(out, "diff", thread.QuotedLines)
	}

	for _, message := range params.Thread.Messages {
		if message.External != nil {
			writeExternal(out, message)

			continue
		}

		out.WriteString(strings.TrimSpace(message.Body))
		out.WriteString("\n\n")

		if len(message.Suggestion) > 0 {
			out.WriteString(suggestionLead(params.ByHuman))
			out.WriteString("\n\n")
			writeFenced(out, "", message.Suggestion)
		}
	}
}

// writeFenced writes lines as a code block whose fence outruns every backtick run inside it.
func writeFenced(out *strings.Builder, info string, lines []string) {
	longest := 0
	for _, line := range lines {
		for _, run := range strings.FieldsFunc(line, func(char rune) bool { return char != '`' }) {
			longest = max(longest, len(run))
		}
	}

	fence := strings.Repeat("`", max(longest+1, 3))

	out.WriteString(fence + info + "\n")
	for _, line := range lines {
		out.WriteString(line)
		out.WriteString("\n")
	}
	out.WriteString(fence + "\n\n")
}

func writeExternal(out *strings.Builder, message reviewstore.Message) {
	author := "Someone"
	if message.External.Login != "" {
		author = "@" + message.External.Login
	}

	fmt.Fprintf(out, "%s wrote this on the forge, and the person reviewing passed it on:\n\n", author)
	for line := range strings.Lines(strings.TrimSpace(message.Body)) {
		out.WriteString("    ")
		out.WriteString(strings.TrimRight(line, "\n"))
		out.WriteString("\n")
	}
	out.WriteString("\n")
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

// hasContext reports whether the snapshot's file holds the lines a comment was written about.
func hasContext(thread reviewstore.Thread, lines []string) bool {
	start := int(thread.NewStart)

	return start > 0 && start <= len(lines)
}

// contextOf is the file around the lines a comment was written about as the snapshot froze them,
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

func oldSideSentence(thread reviewstore.Thread, snapshot reviewstore.Snapshot) string {
	base := thread.BaseSHA
	if base == "" {
		base = snapshot.BaseSHA
	}

	return fmt.Sprintf("This thread's line numbers are on the old side, counted in `%s` rather than "+
		"in the snapshot. To see those lines, run `git diff %s %s`.",
		xsha.Short(base), base, snapshot.SHA)
}

// reviewerName is who gave the review, spelled for somebody reading the file long afterwards,
// an empty author meaning the person running komrad.
func reviewerName(author string) string {
	if author == "" {
		return "the person running komrad"
	}

	return "the agent " + author
}

// verdictSentenceParams is what a review concluded and whether a person gave it.
type verdictSentenceParams struct {
	Verdict reviewstore.Verdict
	Person  bool
}

// verdictSentence opens the file with what the review asks of the agent.
func verdictSentence(params verdictSentenceParams) string {
	if params.Person {
		return "Comments to act on. Answer each one with addressed set once you have made the change."
	}

	switch params.Verdict {
	case reviewstore.VerdictApproved:
		return "Approved. This snapshot is good as it stands."
	case reviewstore.VerdictChangesRequested:
		return "Changes requested. Keep working on this."
	case reviewstore.VerdictCommented:
	}

	return "Comments only, with no conclusion either way."
}
