package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
)

// reviewVerdicts is what the release panel offers, in the order a reader most often wants them.
var reviewVerdicts = []agentv1.ReviewVerdict{
	agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
	agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
	agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED,
}

// composeReview opens the editor on the line under the cursor, replying in the thread already
// there or opening a new one, writing prose alone rather than editing the round.
func (m model) composeReview() (tea.Model, tea.Cmd) {
	// Explainer: reading walks the rounds and writes nothing, so the compose key is also the way
	// into the mode that can. The switch reloads the whole change and puts the cursor back on the
	// line it was pressed on, which is why one press still means one thing.
	if m.review.mode != modeReviewing {
		return m.enterReviewWriting(intentComment)
	}

	thread, replying := m.reviewThreadAtCursor()

	// A thread whose code is gone has no line to anchor a new thread to, but replying to it is
	// what it is still on screen for.
	if _, ok := m.review.view.GetAnchor(m.review.cursor); !ok && !replying {
		return m.refuseReviewLine()
	}

	m.review.editing = ""
	if replying {
		m.review.editing = thread.GetId()
	}

	m.review.composing = true
	m.review.editor.SetValue("")
	m.review.editor.SetWidth(max(20, m.paneWidth()-4))

	return m, m.review.editor.Focus()
}

// enterReviewWriting moves from reading to writing, keeping the reader on the line they pressed
// the key on, and refuses when that line is in a round the whole change does not draw.
func (m model) enterReviewWriting(intent passIntent) (tea.Model, tea.Cmd) {
	// Explainer: an older round draws code the agent has since rewritten, so the line under the
	// cursor has no counterpart in the work a comment could ask it to change. Carrying the reader
	// across would land them on whatever the anchor now matches, which is not the line they chose.
	if said, history := m.isReviewHistory(); history {
		return m.refuseReviewHistory(said)
	}

	if _, ok := m.review.view.GetAnchor(m.review.cursor); !ok {
		if _, replying := m.reviewThreadAtCursor(); !replying {
			return m.refuseReviewLine()
		}
	}

	return m.startReviewPass(intent)
}

// refuseReviewLine says why nothing opened on a row that is not a line of code.
func (m model) refuseReviewLine() (tea.Model, tea.Cmd) {
	m.review.notice = "put the cursor on a line of the diff to comment on it"

	return m, nil
}

// leaveReviewWriting puts the pass down, keeping whatever has been written and letting the agent
// go.
func (m model) leaveReviewWriting() (tea.Model, tea.Cmd) {
	// Comments already written are held by the panel rather than by the pass, so leaving keeps
	// them and says so, which is the difference between putting a review down and losing it.
	if m.doesReviewHoldUnsent() {
		m.review.notice = counted(m.reviewUnsentCount(), "comment") + " kept, still unsent"
	}

	return m.endReviewPass(abandonedSaid)
}

func (m model) updateReviewComposer(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// Every other key is a character being typed into the message, so the keys that leave it, and
	// the one that opens it in an editor, come from the form keymap.
	switch {
	case isBound(k, formKeys().Back):
		return m.cancelReviewMessage(), nil
	case isBound(k, formKeys().Quit):
		return m, m.hide()
	case isBound(k, formKeys().Editor):
		return m.openMessageEditor()
	case isBound(k, formKeys().Submit):
		return m.saveReviewMessage()
	}

	editor, cmd := m.review.editor.Update(key)
	m.review.editor = editor

	return m, cmd
}

// messageEditedMsg is what the reviewer wrote in their own editor.
type messageEditedMsg struct {
	agentID string
	body    string
	err     error
}

// openMessageEditor hands what has been typed so far to the reviewer's editor.
func (m model) openMessageEditor() (tea.Model, tea.Cmd) {
	agentID := m.review.agentID
	path := messageDraftPath(agentID)

	if err := os.WriteFile(path, []byte(m.review.editor.Value()), 0o600); err != nil {
		m.report("Your editor could not be opened on that message", err)

		return m, nil
	}

	// The message is one file with nothing above it, so the editor opens on its first line.
	//nolint:noctx // the editor runs until the reviewer closes it, so it takes no deadline
	open := exec.Command("sh", "-c", editorShell(m.editor, path, 1))

	return m, tea.ExecProcess(open, func(err error) tea.Msg {
		if err != nil {
			return messageEditedMsg{agentID: agentID, err: err}
		}

		body, err := os.ReadFile(path)

		return messageEditedMsg{
			agentID: agentID,
			body:    string(body),
			err:     err,
		}
	})
}

// messageDraftPath is where the message is put for the editor to open.
func messageDraftPath(agentID string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("weave-comment-%s-%d.md", agentID, os.Getpid()))
}

// applyMessageEdited puts what the editor wrote back into the composer.
func (m *model) applyMessageEdited(msg messageEditedMsg) {
	if msg.agentID != m.review.agentID || (!m.review.composing && !m.review.releasing) {
		return
	}

	if msg.err != nil {
		m.report("What you wrote in your editor could not be read back", msg.err)

		return
	}

	m.review.editor.SetValue(strings.TrimRight(msg.body, "\n"))
	m.review.editor.CursorEnd()
}

func (m model) saveReviewMessage() (tea.Model, tea.Cmd) {
	body := strings.TrimSpace(m.review.editor.Value())
	if body == "" {
		return m.cancelReviewMessage(), nil
	}

	m.review.editor.Blur()
	m.review.composing = false

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

	return m, m.createReviewThreadCmd(*anchor, body)
}

// cancelReviewMessage puts the composer away, abandoning nothing but the words typed into it.
func (m model) cancelReviewMessage() model {
	m.review.editor.Blur()
	m.review.composing = false

	return m
}

func (m model) createReviewThreadCmd(anchor review.Anchor, body string) tea.Cmd {
	agents, agentID := m.api.agents, m.review.agentID

	// The old side of these numbers came from whatever the diff was measured from, so the thread
	// records that rather than leaving a later reader to assume the round's own base.
	base := m.review.view.GetBaseSHA()

	// The new side names the snapshot it was read from, which the service compares against the
	// round it would file the thread under rather than trusting the numbers.
	snapshot := m.review.view.GetSnapshotSHA()
	if snapshot == "" {
		snapshot = m.review.round.GetSnapshotSha()
	}

	return m.reviewWriteCmd("thread opened", func(ctx context.Context) error {
		_, err := agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
			AgentId:     agentID,
			BaseSha:     base,
			SnapshotSha: snapshot,
			FilePath:    anchor.FilePath,
			OldStart:    int32(anchor.OldStart),
			OldEnd:      int32(anchor.OldEnd),
			NewStart:    int32(anchor.NewStart),
			NewEnd:      int32(anchor.NewEnd),
			QuotedLines: anchor.QuotedLines,
			Body:        body,
		}))

		return err
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
// again, stepping on to the next file when it has just been folded.
func (m model) toggleReviewViewed() (tea.Model, tea.Cmd) {
	file, ok := m.review.view.GetFileAt(m.review.cursor)
	if !ok {
		return m, nil
	}

	path := file.Path()
	viewed := !m.review.view.IsViewed(path)

	// Where the cursor is standing is taken before the rows are built again.
	place, located := m.review.view.GetPlace(m.review.cursor)
	m.review.view.SetFileViewed(path, viewed)
	m.keepReviewCursor(place, located)

	notice := "file opened again"
	if viewed {
		notice = "file viewed"
		m.review.cursor = m.review.view.JumpFile(m.review.cursor, 1)
		m.review.pinTop = false
	}

	m.scrollReviewToCursor()

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

// markReviewThreadReadCmd claims the thread the cursor has just landed on and only that one.
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

// openReviewRelease asks what to send rather than sending.
func (m model) openReviewRelease() (tea.Model, tea.Cmd) {
	m.review.releasing = true
	m.review.verdict = 0
	m.review.notice = ""
	m.review.editor.SetValue("")
	m.review.editor.SetWidth(max(20, m.paneWidth()-4))

	return m, m.review.editor.Focus()
}

// updateReviewRelease takes every key while the panel is up: tab walks the verdict, the submit
// key sends, escape abandons, and everything else is a word being typed about the work.
func (m model) updateReviewRelease(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, formKeys().Back):
		return m.closeReviewRelease(), nil
	case isBound(k, formKeys().Quit):
		return m, m.hide()
	case k == "tab":
		m.review.verdict = (m.review.verdict + 1) % len(reviewVerdicts)

		return m, nil
	case k == "shift+tab":
		m.review.verdict = (m.review.verdict + len(reviewVerdicts) - 1) % len(reviewVerdicts)

		return m, nil
	case isBound(k, formKeys().Editor):
		return m.openMessageEditor()
	case isBound(k, formKeys().Submit):
		return m.sendReview(false)
	case isBound(k, formKeys().SendNow):
		return m.sendReview(true)
	}

	editor, cmd := m.review.editor.Update(key)
	m.review.editor = editor

	return m, cmd
}

// sendReview releases the batch with the verdict chosen and whatever was written about the work
// as a whole, reaching the agent as one file and one piece of mail.
func (m model) sendReview(interrupt bool) (tea.Model, tea.Cmd) {
	verdict := reviewVerdicts[m.review.verdict]
	body := strings.TrimSpace(m.review.editor.Value())

	m = m.closeReviewRelease()

	// Explainer: the pass existed for the batch that has just gone, so it ends with it rather than
	// parking the reader in a pass whose own keys have withdrawn with the review. Sending is also
	// what lets the agent go, since the comments are now in its hands and answering them is the
	// next thing it does.
	next, letGo := m.endReviewPass(sentSaid)
	m, _ = next.(model)

	agents, agentID := m.api.agents, m.review.agentID

	sent := m.reviewWriteNoticeCmd(func(ctx context.Context) (string, error) {
		resp, err := agents.ReleaseReview(ctx, connect.NewRequest(&agentv1.ReleaseReviewRequest{
			AgentId:   agentID,
			Verdict:   verdict,
			Interrupt: interrupt,
			Body:      body,
		}))
		if err != nil {
			return "", err
		}

		return releasedNotice(verdict, resp.Msg.GetComments(), interrupt), nil
	})

	// The review goes first, so an agent let go before its comments landed cannot start writing
	// over the lines they were pinned to.
	return m, tea.Sequence(sent, letGo)
}

// abandonedSaid is what an agent is told when a pass ends without a review.
const abandonedSaid = "I paused you while I read your work and have let you go now. " +
	"Nothing was sent, so there is nothing of mine to answer."

// sentSaid is what an agent is told when the pass ended by sending it a review.
const sentSaid = "I paused you while I reviewed your work and have let you go now. " +
	"My comments are pinned to your work as it stood when I started, so read them against " +
	"the worktree as it is rather than assuming the lines still match."

func (m model) closeReviewRelease() model {
	m.review.editor.Blur()
	m.review.releasing = false

	return m
}

// releasedNotice says what went.
func releasedNotice(verdict agentv1.ReviewVerdict, count int32, interrupt bool) string {
	sent := ", " + counted(int(count), "comment") + " sent"
	if interrupt {
		sent += " and the agent stopped to read them"
	}

	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return "approved" + sent
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return "changes requested" + sent
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED:
		return "comments only" + sent
	case agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
	}

	return "no verdict" + sent
}

// reviewWriteCmd runs one write and reads the review back in the same breath.
func (m model) reviewWriteCmd(notice string, write func(context.Context) error) tea.Cmd {
	return m.reviewWriteNoticeCmd(func(ctx context.Context) (string, error) {
		return notice, write(ctx)
	})
}

// reviewWriteNoticeCmd is the same for a write whose notice is only known once it has run.
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
