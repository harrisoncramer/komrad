package eventprompts

import (
	"fmt"
	"strings"

	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
)

// ReviewReleased tells a task's writer that a reviewer released a review of its work.
type ReviewReleased struct {
	ReviewID string
	Ordinal  int32
	Verdict  reviewstore.Verdict
	Comments int
	Replies  int
	Path     string
	HasBody  bool
	// ForgeLogin is the forge reviewer whose mirrored comments were released, blank for a review
	// written in komrad.
	ForgeLogin string
}

// Kind is the kind review mail is filed under.
func (ReviewReleased) Kind() mailstore.Kind {
	return mailstore.KindReviewActivity
}

// Events is the event the release starts.
func (m ReviewReleased) Events() []string {
	switch {
	case m.ForgeLogin != "":
		return []string{reviewForgeCommented}
	case m.Verdict == reviewstore.VerdictApproved:
		return []string{ReviewApproved}
	case m.Verdict == reviewstore.VerdictChangesRequested:
		return []string{ReviewChangesRequested}
	default:
		return []string{reviewCommented}
	}
}

// Body names the snapshot, the verdict, how much there is and how to read it.
func (m ReviewReleased) Body() string {
	said := fmt.Sprintf("%d comment(s)", m.Comments)
	if m.Replies > 0 {
		said += fmt.Sprintf(", %d reply(ies) in earlier threads", m.Replies)
	}

	if m.ForgeLogin != "" {
		return fmt.Sprintf("komrad: %s from @%s on the forge, on snapshot %d. "+
			"They are not from the person who runs you. "+
			"Call get_review with review id %s and answer each thread with add_review_message. Never post to the forge.",
			said, m.ForgeLogin, m.Ordinal, m.ReviewID)
	}

	said = strings.ReplaceAll(string(m.Verdict), "_", " ") + ", " + said

	if m.Verdict == reviewstore.VerdictApproved && m.Comments == 0 && m.Replies == 0 && !m.HasBody {
		return fmt.Sprintf("komrad: review of snapshot %d released: %s. There is nothing to read or answer.", m.Ordinal, said)
	}

	return fmt.Sprintf("komrad: review of snapshot %d released: %s. "+
		"Call get_review with review id %s, or read %s, and answer each thread with add_review_message.",
		m.Ordinal, said, m.ReviewID, m.Path)
}

// ReviewComment carries one comment the person wrote on a task's work to its writer, whole.
type ReviewComment struct {
	ThreadID string
	// Ordinal is the snapshot the comment's lines were read in.
	Ordinal int32
	// Anchor is the file and lines the thread is pinned to, as the review file heads it, empty for
	// a comment on the change as a whole.
	Anchor string
	// QuotedLines is the diff the thread was opened on, empty on a reply and on a comment on the
	// change as a whole.
	QuotedLines []string
	Suggestion  []string
	// Said is the comment itself.
	Said string
	// Reply is whether the comment answers in a thread the writer was told of before.
	Reply bool
	// ForgeLogin is the forge user whose comment the person passed on, blank for the person's own
	// words.
	ForgeLogin string
}

// Kind is the kind a person's comment is filed under.
func (ReviewComment) Kind() mailstore.Kind {
	return mailstore.KindReviewComment
}

// Events is the event a person's comment starts.
func (ReviewComment) Events() []string {
	return []string{ReviewCommentedByPerson}
}

// Body is the comment itself, where it is pinned, and how to answer it.
func (m ReviewComment) Body() string {
	var out strings.Builder

	fmt.Fprintf(&out, "komrad: %s %s, thread %s.\n\n", m.who(), m.where(), m.ThreadID)

	if len(m.QuotedLines) > 0 {
		WriteFenced(&out, "diff", m.QuotedLines)
	}

	out.WriteString(strings.TrimSpace(m.Said))
	out.WriteString("\n")

	if len(m.Suggestion) > 0 {
		out.WriteString("\nThe person wrote this replacement for those lines themselves. Apply it as written. " +
			"If it no longer applies because the code was renamed or removed, say so in the thread rather than half-applying it:\n")
		WriteFenced(&out, "", m.Suggestion)
	}

	out.WriteString("\nAnswer in the thread with add_review_message, naming its id, with addressed set once you have made the change.")

	return out.String()
}

// who says whose words the comment is and whether it opens a thread or answers in one.
func (m ReviewComment) who() string {
	if m.ForgeLogin != "" {
		return fmt.Sprintf("the person passed on a comment @%s wrote on the forge", m.ForgeLogin)
	}
	if m.Reply {
		return "the person replied"
	}

	return "the person commented"
}

// where names the snapshot and the lines the comment is pinned to, or the change as a whole.
func (m ReviewComment) where() string {
	if m.Anchor == "" {
		return fmt.Sprintf("on the whole change as of snapshot %d", m.Ordinal)
	}

	return fmt.Sprintf("on snapshot %d at %s", m.Ordinal, m.Anchor)
}

// WriteFenced writes lines as a code block whose fence outruns every backtick run inside it.
func WriteFenced(out *strings.Builder, info string, lines []string) {
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
	out.WriteString(fence + "\n")
}

// ThreadAnswered tells a reviewer that the writer it reviews answered its review.
type ThreadAnswered struct {
	WriterName string
}

// Kind is the kind review mail is filed under.
func (ThreadAnswered) Kind() mailstore.Kind {
	return mailstore.KindReviewActivity
}

// Events is the event an answer starts.
func (ThreadAnswered) Events() []string {
	return []string{ReviewAnswered}
}

// Body asks the reviewer to read the answer and release a verdict on it.
func (m ThreadAnswered) Body() string {
	return fmt.Sprintf("komrad: %s answered your review. Read the threads with list_review_threads, "+
		"take a snapshot of its work as it now stands, and release a verdict on that snapshot.", m.WriterName)
}
