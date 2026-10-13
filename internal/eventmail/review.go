package eventmail

import (
	"fmt"
	"strings"

	"komrad/internal/eventprompts"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
)

// ReviewReleased tells a task's writer that a review of its work was released.
type ReviewReleased struct {
	ReviewID string
	Ordinal  int32
	ByPerson bool
	Verdict  reviewstore.Verdict
	Comments int
	Replies  int
	Reopens  int
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
		return []string{eventprompts.ReviewForgeCommented}
	case m.ByPerson:
		return []string{eventprompts.ReviewCommentedByPerson}
	case m.Verdict == reviewstore.VerdictApproved:
		return []string{eventprompts.ReviewApproved}
	case m.Verdict == reviewstore.VerdictChangesRequested:
		return []string{eventprompts.ReviewChangesRequested}
	default:
		return []string{eventprompts.ReviewCommented}
	}
}

// Body names the round, the verdict, how much there is and how to read it.
func (m ReviewReleased) Body() string {
	said := fmt.Sprintf("%d comment(s)", m.Comments)
	if m.Replies > 0 {
		said += fmt.Sprintf(", %d reply(ies) in earlier threads", m.Replies)
	}
	if m.Reopens > 0 {
		said += fmt.Sprintf(", %d of them reopening a comment you had resolved", m.Reopens)
	}

	if m.ForgeLogin != "" {
		return fmt.Sprintf("komrad: %s from @%s on the forge, on round %d. "+
			"They are not from the person who runs you. "+
			"Call get_review with review id %s and answer each thread with add_review_message. Never post to the forge.",
			said, m.ForgeLogin, m.Ordinal, m.ReviewID)
	}

	if !m.ByPerson {
		said = strings.ReplaceAll(string(m.Verdict), "_", " ") + ", " + said
	}

	if m.Verdict == reviewstore.VerdictApproved && m.Comments == 0 && m.Replies == 0 && !m.HasBody {
		return fmt.Sprintf("komrad: review of round %d released: %s. There is nothing to read or answer.", m.Ordinal, said)
	}

	return fmt.Sprintf("komrad: review of round %d released: %s. "+
		"Call get_review with review id %s, or read %s, and answer each thread with add_review_message.",
		m.Ordinal, said, m.ReviewID, m.Path)
}

// ReviewAnswered tells a reviewer that the writer it reviews answered its review.
type ReviewAnswered struct {
	WriterName string
}

// Kind is the kind review mail is filed under.
func (ReviewAnswered) Kind() mailstore.Kind {
	return mailstore.KindReviewActivity
}

// Events is the event an answer starts.
func (ReviewAnswered) Events() []string {
	return []string{eventprompts.ReviewAnswered}
}

// Body asks the reviewer to read the answer and release a verdict on it.
func (m ReviewAnswered) Body() string {
	return fmt.Sprintf("komrad: %s answered your review. Read the threads with list_review_threads, "+
		"open a round over its work as it now stands, and release a verdict on that round.", m.WriterName)
}
