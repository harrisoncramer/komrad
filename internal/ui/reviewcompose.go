package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"weave/internal/ui/diffview"
	"weave/internal/ui/paint/label"
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

// compose opens the editor on the line under the cursor, replying in the thread already there
// or opening a new one, writing prose alone rather than editing the round.
func (r *reviewScreen) compose(h host, s panelShared) tea.Cmd {
	if r.pass.mode != modeReviewing {
		return r.enterWriting(h, s, intentComment)
	}

	thread, replying := r.diff.threadAtCursor()

	// A thread whose code is gone has no line to anchor a new thread to, but replying to it is
	// what it is still on screen for.
	if _, ok := r.diff.view.GetAnchor(r.diff.cursor); !ok && !replying {
		r.refuseLine()

		return nil
	}

	r.threads.editing = ""
	if replying {
		r.threads.editing = thread.GetId()
	}

	r.threads.composing = true

	return r.openInput(s, newReviewInputParams{
		Title:       reviewInputTitle(replying),
		Description: reviewInputHint(formKeys().Editor, formKeys().Submit, "save"),
		Placeholder: "what should the agent change here?",
	})
}

func reviewInputTitle(replying bool) string {
	if replying {
		return "Reply"
	}

	return "Comment"
}

// reviewInputWidth is the room a field has in a sidebar of the width given.
func reviewInputWidth(pane int) int {
	return max(pane-reviewInputBar, 1)
}

// reviewInputHint is the line under a field's title, naming the keys that finish with it.
func reviewInputHint(editor, submit []string, said string) string {
	return keyLabel(editor) + " opens $EDITOR; " + keyLabel(submit) + " " + said + "; " +
		keyLabel(formKeys().Back) + " cancels"
}

// openInput puts a field in the sidebar, sized to the column it is drawn in.
func (r *reviewScreen) openInput(s panelShared, params newReviewInputParams) tea.Cmd {
	params.Width = reviewInputWidth(s.pane)
	params.Keys = formKeys()

	input, cmd := newReviewInput(params)
	r.threads.editor = input

	return cmd
}

// enterWriting moves from reading to writing, keeping the reader on the line they pressed the
// key on, and refuses when that line is in a round the whole change does not draw.
func (r *reviewScreen) enterWriting(h host, s panelShared, intent passIntent) tea.Cmd {
	if said, history := r.isHistory(); history {
		r.refuseHistory(h, s, said)

		return nil
	}

	if _, ok := r.diff.view.GetAnchor(r.diff.cursor); !ok {
		if _, replying := r.diff.threadAtCursor(); !replying {
			r.refuseLine()

			return nil
		}
	}

	return r.startPass(h, s, intent)
}

// refuseLine says why nothing opened on a row that is not a line of code.
func (r *reviewScreen) refuseLine() {
	r.notice = "put the cursor on a line of the diff to comment on it"
}

// leaveWriting puts the pass down, keeping whatever has been written and letting the agent go.
func (r *reviewScreen) leaveWriting(h host, s panelShared) tea.Cmd {
	// Comments already written are held by the panel rather than by the pass, so leaving keeps
	// them and says so.
	if r.threads.holdsUnsent() {
		r.notice = label.Counted(r.threads.unsentCount(), "comment") + " kept, still unsent"
	}

	return r.endPass(h, s, abandonedSaid)
}

func (r *reviewScreen) updateComposer(h host, s panelShared, key tea.KeyMsg) tea.Cmd {
	k := key.String()

	// Every other key is a character being typed into the message, so the keys that leave it, and
	// the one that opens it in an editor, come from the form keymap.
	switch {
	case isBound(k, formKeys().Back):
		r.threads.cancelMessage()

		return nil
	case isBound(k, formKeys().Quit):
		return h.hide()
	case isBound(k, formKeys().Editor):
		return r.openMessageEditor(h, s)
	case isBound(k, formKeys().Submit):
		return r.saveMessage(s)
	}

	editor, cmd := r.threads.editor.update(key)
	r.threads.editor = editor

	return cmd
}

// messageEditedMsg is what the reviewer wrote in their own editor.
type messageEditedMsg struct {
	agentID string
	body    string
	err     error
}

// openMessageEditor hands what has been typed so far to the reviewer's editor.
func (r reviewScreen) openMessageEditor(h host, s panelShared) tea.Cmd {
	agentID := r.agentID
	path := messageDraftPath(agentID)

	if err := os.WriteFile(path, []byte(r.threads.editor.value()), 0o600); err != nil {
		h.report("Your editor could not be opened on that message", err)

		return nil
	}

	// The message is one file with nothing above it, so the editor opens on its first line.
	//nolint:noctx // the editor runs until the reviewer closes it, so it takes no deadline
	open := exec.Command("sh", "-c", editorShell(s.editor, path, 1))

	return tea.ExecProcess(open, func(err error) tea.Msg {
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
func (r *reviewScreen) applyMessageEdited(h host, s panelShared, msg messageEditedMsg) {
	if msg.agentID != r.agentID || (!r.threads.composing && !r.threads.releasing) {
		return
	}

	if msg.err != nil {
		h.report("What you wrote in your editor could not be read back", msg.err)

		return
	}

	said, key := "save", formKeys().Submit
	if r.threads.releasing {
		said = "send"
	}

	input, _ := newReviewInput(newReviewInputParams{
		Title:       reviewInputTitle(r.threads.editing != ""),
		Description: reviewInputHint(formKeys().Editor, key, said),
		Body:        strings.TrimRight(msg.body, "\n"),
		Width:       reviewInputWidth(s.pane),
		Keys:        formKeys(),
	})
	r.threads.editor = input
}

func (r *reviewScreen) saveMessage(s panelShared) tea.Cmd {
	body := strings.TrimSpace(r.threads.editor.value())
	if body == "" {
		r.threads.cancelMessage()

		return nil
	}

	r.threads.editor = reviewInput{}
	r.threads.composing = false

	agents, threadID := s.api.agents, r.threads.editing
	if threadID != "" {
		// Nothing is said back: the reply is drawn in the thread it went into.
		return r.writeCmd(s, "", func(ctx context.Context) error {
			_, err := agents.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{
				ThreadId: threadID,
				Body:     body,
			}))

			return err
		})
	}

	anchor, ok := r.diff.view.GetAnchor(r.diff.cursor)
	if !ok {
		return nil
	}

	return r.createThreadCmd(s, *anchor, body)
}

// cancelMessage puts the composer away, abandoning nothing but the words typed into it.
func (t *reviewThreads) cancelMessage() {
	t.editor = reviewInput{}
	t.composing = false
}

func (r reviewScreen) createThreadCmd(s panelShared, anchor diffview.Anchor, body string) tea.Cmd {
	agents, agentID := s.api.agents, r.agentID

	// The old side of these numbers came from whatever the diff was measured from, so the thread
	// records that rather than leaving a later reader to assume the round's own base.
	base := r.diff.view.GetBaseSHA()

	// The new side names the snapshot it was read from, which the service compares against the
	// round it would file the thread under rather than trusting the numbers.
	snapshot := r.diff.view.GetSnapshotSHA()
	if snapshot == "" {
		snapshot = r.round.GetSnapshotSha()
	}

	return r.writeCmd(s, "thread opened", func(ctx context.Context) error {
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

// resolveThread settles the thread the cursor is standing on, or reopens one already settled.
func (r reviewScreen) resolveThread(s panelShared) tea.Cmd {
	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	agents, id, settling := s.api.agents, thread.GetId(), thread.GetResolvedAt() == ""

	notice := "thread reopened"
	if settling {
		notice = "thread settled"
	}

	return r.writeCmd(s, notice, func(ctx context.Context) error {
		_, err := agents.SetReviewThreadResolved(ctx, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{
			Id:       id,
			Resolved: settling,
		}))

		return err
	})
}

// toggleViewed folds the file the cursor is in away once it has been read, or opens it again,
// stepping on to the next file when it has just been folded.
func (r *reviewScreen) toggleViewed(h host, s panelShared) tea.Cmd {
	if r.diff.isNarrowed() {
		r.refuseViewed(h, s)

		return nil
	}

	file, ok := r.diff.view.GetPatchFileAt(r.diff.cursor)
	if !ok {
		return nil
	}

	path := file.Path()
	viewed := !r.diff.view.IsViewed(path)

	// Where the cursor is standing is taken before the rows are built again.
	place, located := r.diff.view.GetPlace(r.diff.cursor)
	r.diff.view.SetFileViewed(path, viewed)
	r.diff.keepCursor(place, located)

	notice := "file opened again"
	if viewed {
		notice = "file viewed"
		r.diff.cursor = r.diff.view.JumpFile(r.diff.cursor, 1)
		r.diff.pinTop = false
	}

	r.scrollToCursor(s)

	agents, agentID := s.api.agents, r.agentID
	fingerprint := file.Fingerprint()

	return r.writeCmd(s, notice, func(ctx context.Context) error {
		_, err := agents.SetReviewFileViewed(ctx, connect.NewRequest(&agentv1.SetReviewFileViewedRequest{
			AgentId:     agentID,
			FilePath:    path,
			Fingerprint: fingerprint,
			Viewed:      viewed,
		}))

		return err
	})
}

// markThreadReadCmd claims the thread the cursor has just landed on and only that one.
func (r reviewScreen) markThreadReadCmd(s panelShared) tea.Cmd {
	thread, ok := r.diff.threadAtCursor()
	if !ok || !thread.GetUnreadByHuman() {
		return nil
	}

	agents, id := s.api.agents, thread.GetId()

	return r.writeCmd(s, "", func(ctx context.Context) error {
		_, err := agents.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: id}))

		return err
	})
}

// openRelease asks what to send rather than sending.
func (r *reviewScreen) openRelease(s panelShared) tea.Cmd {
	r.threads.releasing = true
	r.threads.verdict = 0
	r.notice = ""

	return r.openInput(s, newReviewInputParams{
		Title:       "Anything about the work as a whole?",
		Description: reviewInputHint(formKeys().Editor, formKeys().Submit, "sends"),
		Placeholder: "optional",
	})
}

// updateRelease takes every key while the panel is up: tab walks the verdict, the submit key
// sends, escape abandons, and everything else is a word being typed about the work.
func (r *reviewScreen) updateRelease(h host, s panelShared, key tea.KeyMsg) tea.Cmd {
	k := key.String()

	switch {
	case isBound(k, formKeys().Back):
		r.threads.closeRelease()

		return nil
	case isBound(k, formKeys().Quit):
		return h.hide()
	case k == "tab":
		r.threads.cycleVerdict(1)

		return nil
	case k == "shift+tab":
		r.threads.cycleVerdict(-1)

		return nil
	case isBound(k, formKeys().Editor):
		return r.openMessageEditor(h, s)
	case isBound(k, formKeys().Submit):
		return r.send(s, false)
	case isBound(k, formKeys().SendNow):
		return r.send(s, true)
	}

	editor, cmd := r.threads.editor.update(key)
	r.threads.editor = editor

	return cmd
}

// send releases the batch with the verdict chosen and whatever was written about the work as a
// whole, reaching the agent as one file and one piece of mail.
func (r *reviewScreen) send(s panelShared, interrupt bool) tea.Cmd {
	verdict := reviewVerdicts[r.threads.verdict]
	body := strings.TrimSpace(r.threads.editor.value())

	r.threads.closeRelease()

	agents, agentID := s.api.agents, r.agentID

	return r.sendCmd(s, func(ctx context.Context) (string, error) {
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
}

// abandonedSaid is what an agent is told when a pass ends without a review.
const abandonedSaid = "I paused you while I read your work and have let you go now. " +
	"Nothing was sent, so there is nothing of mine to answer."

// sentSaid is what an agent is told when the pass ended by sending it a review.
const sentSaid = "I paused you while I reviewed your work and have let you go now. " +
	"My comments are pinned to your work as it stood when I started, so read them against " +
	"the worktree as it is rather than assuming the lines still match."

// cycleVerdict moves the choice of verdict one step round the list.
func (t *reviewThreads) cycleVerdict(delta int) {
	t.verdict = (t.verdict + delta + len(reviewVerdicts)) % len(reviewVerdicts)
}

// closeRelease puts the release panel away, sent or not.
func (t *reviewThreads) closeRelease() {
	t.editor = reviewInput{}
	t.releasing = false
}

// releasedNotice says what went.
func releasedNotice(verdict agentv1.ReviewVerdict, count int32, interrupt bool) string {
	sent := ", " + label.Counted(int(count), "comment") + " sent"
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

// writeCmd runs one write on the review.
func (r reviewScreen) writeCmd(s panelShared, notice string, write func(context.Context) error) tea.Cmd {
	return r.writeMsgCmd(s, false, func(ctx context.Context) (string, error) {
		return notice, write(ctx)
	})
}

// sendCmd is the write that releases a review.
func (r reviewScreen) sendCmd(s panelShared, write func(context.Context) (string, error)) tea.Cmd {
	return r.writeMsgCmd(s, true, write)
}

// writeMsgCmd runs one write, marking whose answer it is, and reads an archived agent's threads back.
func (r reviewScreen) writeMsgCmd(s panelShared, sent bool, write func(context.Context) (string, error)) tea.Cmd {
	agents, agentID := s.api.agents, r.agentID
	_, live := r.agentRow(s)

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		notice, err := write(ctx)
		wrote := reviewWroteMsg{
			agentID: agentID,
			notice:  notice,
			err:     err,
			sent:    sent,
		}
		if err != nil || live {
			return wrote
		}

		wrote.readBack = true
		wrote.threads, wrote.readErr = listReviewThreads(ctx, agents, agentID)

		return wrote
	}
}
