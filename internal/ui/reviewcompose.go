package ui

import (
	"context"
	"strings"

	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
)

// reviewVerdicts is what the release panel offers, in the order a reader most often wants them,
// so the first is the one the submit key used to send on its own.
var reviewVerdicts = []agentv1.ReviewVerdict{
	agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
	agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
	agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED,
}

// composeReview opens the editor on the line under the cursor, replying in the thread already
// there, opening a new one, or carrying an edit of the round that is waiting to be sent, which
// takes precedence over both since it is pinned where it was made rather than under the cursor.
func (m model) composeReview() (tea.Model, tea.Cmd) {
	held := len(m.review.suggestions) > 0
	thread, replying := m.reviewThreadAtCursor()
	replying = replying && !held

	// A thread whose code is gone has no line to anchor a new thread to, but replying to it is
	// what it is still on screen for.
	if _, ok := m.review.view.GetAnchor(m.review.cursor); !ok && !replying && !held {
		return m, nil
	}

	m.review.editing = ""
	if replying {
		m.review.editing = thread.GetId()
	}

	m.review.composing = true
	m.review.editor.SetValue("")
	m.review.editor.SetWidth(max(20, m.width-4))

	return m, m.review.editor.Focus()
}

func (m model) updateReviewComposer(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// Every other key is a character being typed into the message, so the two that put the
	// editor away are fixed rather than configured.
	switch {
	case k == "esc":
		return m.cancelReviewMessage(), nil
	case k == "ctrl+c":
		return m, m.hide()
	case isBound(k, m.keys.Form.Submit):
		return m.saveReviewMessage()
	}

	editor, cmd := m.review.editor.Update(key)
	m.review.editor = editor

	return m, cmd
}

func (m model) saveReviewMessage() (tea.Model, tea.Cmd) {
	body := strings.TrimSpace(m.review.editor.Value())
	if body == "" {
		return m.cancelReviewMessage(), nil
	}

	held, sending := m.takeSuggestion()
	m.review.editor.Blur()
	m.review.composing = false

	if sending {
		return m, m.createReviewThreadCmd(held.anchor, held.lines, body)
	}

	agents, threadID := m.api.agents, m.review.editing
	if threadID != "" {
		// Nothing is said back: the reply is drawn in the thread it went into, which is already on
		// the screen the moment it lands.
		return m, m.reviewWriteCmd("", func(ctx context.Context) error {
			_, err := agents.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{
				ThreadId: threadID,
				Body:     body,
			}))

			return err
		})
	}

	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
	if !ok {
		return m, nil
	}

	return m, m.createReviewThreadCmd(*anchor, nil, body)
}

// cancelReviewMessage puts the composer away, turning down the edit it was asking about, since the
// composer is where weave asks whether to send one and leaving it is the answer.
func (m model) cancelReviewMessage() model {
	m.review.editor.Blur()
	m.review.composing = false

	if held, ok := m.takeSuggestion(); ok {
		m.review.notice = "your edit of " +
			review.FormatLocation(held.anchor.FilePath, held.anchor.OldStart, held.anchor.NewStart) +
			" was turned down"
	}

	return m
}

// takeSuggestion lifts the edit the composer is asking about off the front of the queue and
// records that it has been answered for, so reading the checkout again does not offer it back.
func (m *model) takeSuggestion() (pendingSuggestion, bool) {
	if len(m.review.suggestions) == 0 {
		return pendingSuggestion{}, false
	}

	held := m.review.suggestions[0]
	m.review.suggestions = m.review.suggestions[1:]

	if m.review.settled == nil {
		m.review.settled = map[string]bool{}
	}

	m.review.settled[held.key().Key()] = true

	return held, true
}

func (m model) createReviewThreadCmd(anchor review.Anchor, suggestion []string, body string) tea.Cmd {
	agents, agentID := m.api.agents, m.review.agentID

	notice := "thread opened"
	if len(suggestion) > 0 {
		notice = "thread opened with your edit as a suggestion"
	}

	carried := m.carriedEditDrop(anchor, suggestion)

	return m.reviewWriteCmd(notice, func(ctx context.Context) error {
		_, err := agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
			AgentId:     agentID,
			FilePath:    anchor.FilePath,
			OldStart:    int32(anchor.OldStart),
			OldEnd:      int32(anchor.OldEnd),
			NewStart:    int32(anchor.NewStart),
			NewEnd:      int32(anchor.NewEnd),
			QuotedLines: anchor.QuotedLines,
			Suggestion:  suggestion,
			Body:        body,
		}))
		if err != nil {
			return err
		}

		carried(ctx)

		return nil
	})
}

// resolveReviewThread settles the thread the cursor is standing on, or reopens one already
// settled.
func (m model) resolveReviewThread() (tea.Model, tea.Cmd) {
	thread, ok := m.reviewThreadAtCursor()
	if !ok {
		return m, nil
	}

	agents, id, settling := m.api.agents, thread.GetId(), thread.GetResolvedAt() == ""

	notice := "thread reopened"
	if settling {
		notice = "thread settled"
	}

	return m, m.reviewWriteCmd(notice, func(ctx context.Context) error {
		_, err := agents.SetReviewThreadResolved(ctx, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{
			Id:       id,
			Resolved: settling,
		}))

		return err
	})
}

// toggleReviewViewed folds the file the cursor is in away once it has been read, or opens it
// again, stepping on to the next file when it has just been folded, since reviewing is a walk
// down them.
func (m model) toggleReviewViewed() (tea.Model, tea.Cmd) {
	file, ok := m.review.view.GetFileAt(m.review.cursor)
	if !ok {
		return m, nil
	}

	path := file.Path()
	viewed := !m.review.view.IsViewed(path)

	// Where the cursor is standing is taken before the rows are built again, since folding the
	// file away takes the line it was on out of the diff.
	place, located := m.review.view.GetPlace(m.review.cursor)
	m.review.view.SetFileViewed(path, viewed)
	m.keepReviewCursor(place, located)

	notice := "file opened again"
	if viewed {
		notice = "file viewed"
		m.review.cursor = m.review.view.JumpFile(m.review.cursor, 1)
		m.review.pinTop = false
	}

	m.stageReview()

	agents, roundID := m.api.agents, m.review.round.GetId()

	return m, m.reviewWriteCmd(notice, func(ctx context.Context) error {
		_, err := agents.SetReviewFileViewed(ctx, connect.NewRequest(&agentv1.SetReviewFileViewedRequest{
			RoundId:  roundID,
			FilePath: path,
			Viewed:   viewed,
		}))

		return err
	})
}

// markReviewThreadReadCmd claims the thread the cursor has just landed on and only that one,
// since marking the whole list would claim threads were read that were never opened.
func (m model) markReviewThreadReadCmd() tea.Cmd {
	thread, ok := m.reviewThreadAtCursor()
	if !ok || !thread.GetUnreadByHuman() {
		return nil
	}

	agents, id := m.api.agents, thread.GetId()

	return m.reviewWriteCmd("", func(ctx context.Context) error {
		_, err := agents.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: id}))

		return err
	})
}

// openReviewRelease asks what to send rather than sending, since a verdict is a decision worth
// making deliberately and a word about the work as a whole belongs with it.
func (m model) openReviewRelease() (tea.Model, tea.Cmd) {
	m.review.releasing = true
	m.review.verdict = 0
	m.review.notice = ""
	m.review.editor.SetValue("")
	m.review.editor.SetWidth(max(20, m.width-4))

	return m, m.review.editor.Focus()
}

// updateReviewRelease takes every key while the panel is up: tab walks the verdict, the submit
// key sends, escape abandons, and everything else is a word being typed about the work.
func (m model) updateReviewRelease(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case k == "esc":
		return m.closeReviewRelease(), nil
	case k == "ctrl+c":
		return m, m.hide()
	case k == "tab":
		m.review.verdict = (m.review.verdict + 1) % len(reviewVerdicts)

		return m, nil
	case k == "shift+tab":
		m.review.verdict = (m.review.verdict + len(reviewVerdicts) - 1) % len(reviewVerdicts)

		return m, nil
	case isBound(k, m.keys.Form.Submit):
		return m.sendReview()
	}

	editor, cmd := m.review.editor.Update(key)
	m.review.editor = editor

	return m, cmd
}

// sendReview lets the batch go with the verdict chosen and whatever was written about the work as
// a whole, which reaches the agent as one file and one piece of mail.
func (m model) sendReview() (tea.Model, tea.Cmd) {
	verdict := reviewVerdicts[m.review.verdict]
	body := strings.TrimSpace(m.review.editor.Value())

	m = m.closeReviewRelease()

	agents, agentID := m.api.agents, m.review.agentID

	return m, m.reviewWriteNoticeCmd(func(ctx context.Context) (string, error) {
		resp, err := agents.ReleaseReview(ctx, connect.NewRequest(&agentv1.ReleaseReviewRequest{
			AgentId: agentID,
			Verdict: verdict,
			Body:    body,
		}))
		if err != nil {
			return "", err
		}

		return releasedNotice(verdict, resp.Msg.GetComments()), nil
	})
}

func (m model) closeReviewRelease() model {
	m.review.editor.Blur()
	m.review.releasing = false

	return m
}

// releasedNotice says what went, since a review leaving with nothing said about it reads as the
// key not working.
func releasedNotice(verdict agentv1.ReviewVerdict, count int32) string {
	sent := ", " + counted(int(count), "comment") + " sent"

	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return "approved" + sent
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return "changes requested" + sent
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED:
		return "comments only" + sent
	case agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
		return "no verdict" + sent
	default:
		return "no verdict" + sent
	}
}

// reviewWriteCmd runs one write and reads the review back in the same breath.
func (m model) reviewWriteCmd(notice string, write func(context.Context) error) tea.Cmd {
	return m.reviewWriteNoticeCmd(func(ctx context.Context) (string, error) {
		return notice, write(ctx)
	})
}

// reviewWriteNoticeCmd is the same for a write whose notice is only known once it has run,
// since what ringing again did is in the answer rather than in the asking.
func (m model) reviewWriteNoticeCmd(write func(context.Context) (string, error)) tea.Cmd {
	agents, agentID := m.api.agents, m.review.agentID

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		notice, err := write(ctx)
		if err != nil {
			return reviewWroteMsg{agentID: agentID, err: err}
		}

		threads, err := listReviewThreads(ctx, agents, agentID)

		return reviewWroteMsg{
			agentID: agentID,
			notice:  notice,
			threads: threads,
			err:     err,
		}
	}
}
