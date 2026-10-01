package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint/label"
	"komrad/internal/vim"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

// compose opens the editor on the line under the cursor, replying in the thread already there
// or opening a new one, writing prose alone rather than editing the round.
func (r *reviewScreen) compose(h host, s panelShared) tea.Cmd {
	thread, replying := r.diff.threadAtCursor()

	// A thread whose code is gone has no line to anchor a new thread to, but replying to it is
	// what it is still on screen for.
	if _, ok := r.diff.View().GetAnchor(r.diff.Cursor()); !ok && !replying {
		r.refuseLine(h)

		return nil
	}

	r.threads.editing = ""
	if replying {
		r.threads.editing = thread.GetId()
	}

	r.threads.composing = true
	r.threads.retrying = nil

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

// reviewInputWidth is the room a field has in a popup of the inner width given.
func reviewInputWidth(pane int) int {
	return max(pane-reviewInputBar, 1)
}

// reviewInputHint is the line under a field's title, naming the keys that finish with it.
func reviewInputHint(editor, submit []string, said string) string {
	return keyLabel(editor) + " opens $EDITOR; " + keyLabel(submit) + " " + said + "; " +
		keyLabel(formKeys().Back) + " cancels"
}

// openInput puts a field in the popup, sized to the room inside it.
func (r *reviewScreen) openInput(s panelShared, params newReviewInputParams) tea.Cmd {
	params.Width = reviewInputWidth(r.measurePopupInner(s))
	params.Keys = formKeys()
	params.Register = s.register

	input, cmd := newReviewInput(params)
	r.threads.editor = input

	return cmd
}

// refuseLine says why nothing opened on a row that is not a line of code.
func (r *reviewScreen) refuseLine(h host) {
	h.note(messageWarning, "put the cursor on a line of the diff to comment on it")
}

func (r *reviewScreen) updateComposer(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	outcome := r.threads.editor.updateVim(key)
	if outcome != vim.Pass && outcome != vim.Leave {
		return nil
	}

	k := key.String()

	// Every other key is a character being typed into the message, so the keys that leave it, and
	// the one that opens it in an editor, come from the form keymap.
	switch {
	case outcome == vim.Leave || isBound(k, formKeys().Back):
		r.threads.cancelMessage()

		return nil
	case isBound(k, formKeys().Quit):
		return h.hide()
	case isBound(k, formKeys().Editor):
		return r.openMessageEditor(h, s)
	case isBound(k, formKeys().Submit):
		return r.saveMessage(h, s)
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
	return filepath.Join(os.TempDir(), fmt.Sprintf("komrad-comment-%s-%d.md", agentID, os.Getpid()))
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
		Width:       reviewInputWidth(r.measurePopupInner(s)),
		Keys:        formKeys(),
	})
	r.threads.editor = input
}

func (r *reviewScreen) saveMessage(h host, s panelShared) tea.Cmd {
	body := strings.TrimSpace(r.threads.editor.value())
	if body == "" {
		r.threads.cancelMessage()

		return nil
	}

	retrying := r.threads.retrying
	r.threads.editor = reviewInput{}
	r.threads.composing = false
	r.threads.retrying = nil

	agents, threadID := s.api.agents, r.threads.editing
	kept := reviewUnsent{editing: threadID, body: body}
	if threadID != "" {
		// Nothing is said back: the reply is drawn in the thread it went into.
		return keepUnsent(kept, r.writeCmd(s, h.beginWrite("replying"), "", func(ctx context.Context) error {
			_, err := agents.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{
				ThreadId: threadID,
				Body:     body,
			}))

			return err
		}))
	}

	place := retrying.getPlace()
	if place == nil {
		anchor, ok := r.diff.View().GetAnchor(r.diff.Cursor())
		if !ok {
			return nil
		}

		place = &reviewPlace{
			anchor: *anchor,
			// The old side of these numbers came from whatever the diff was measured from, so the
			// thread records that rather than leaving a later reader to assume the round's own base.
			base: r.diff.View().GetBaseSHA(),
			// The new side names the snapshot it was read from, which the service compares against
			// the round it would file the thread under rather than trusting the numbers.
			snapshot: r.diff.View().GetSnapshotSHA(),
		}
		if place.snapshot == "" {
			place.snapshot = r.round.GetSnapshotSha()
		}
	}

	kept.place = place

	return keepUnsent(kept, r.createThreadCmd(h, s, *place, body))
}

// reviewUnsent is a message on its way to the service, held until the write carrying it lands.
type reviewUnsent struct {
	editing string
	body    string
	// place is where a new thread goes, nil for a reply.
	place *reviewPlace
}

func (u *reviewUnsent) getPlace() *reviewPlace {
	if u == nil {
		return nil
	}

	return u.place
}

// reviewPlace is the lines a new thread is pinned to and the snapshots they were read between.
type reviewPlace struct {
	anchor   diffview.Anchor
	base     string
	snapshot string
}

// keepUnsent hands a message back with the refusal of the write carrying it.
func keepUnsent(kept reviewUnsent, write tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		msg := write()

		wrote, ok := msg.(reviewWroteMsg)
		if !ok || wrote.err == nil {
			return msg
		}

		wrote.unsent = &kept

		return wrote
	}
}

// restoreMessage opens the composer again on a message the service refused.
func (r *reviewScreen) restoreMessage(s panelShared, unsent reviewUnsent) tea.Cmd {
	if r.threads.composing || r.threads.releasing {
		return nil
	}

	r.threads.editing = unsent.editing
	r.threads.composing = true
	r.threads.retrying = &unsent

	return r.openInput(s, newReviewInputParams{
		Title:       reviewInputTitle(unsent.editing != ""),
		Description: reviewInputHint(formKeys().Editor, formKeys().Submit, "save"),
		Body:        unsent.body,
	})
}

// cancelMessage puts the composer away, abandoning nothing but the words typed into it.
func (t *reviewThreads) cancelMessage() {
	t.editor = reviewInput{}
	t.composing = false
	t.retrying = nil
}

func (r reviewScreen) createThreadCmd(h host, s panelShared, place reviewPlace, body string) tea.Cmd {
	agents, agentID := s.api.agents, r.agentID
	anchor := place.anchor

	return r.writeCmd(s, h.beginWrite("opening a thread"), "thread opened", func(ctx context.Context) error {
		_, err := agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
			AgentId:     agentID,
			BaseSha:     place.base,
			SnapshotSha: place.snapshot,
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
func (r reviewScreen) resolveThread(h host, s panelShared) tea.Cmd {
	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	agents, id, settling := s.api.agents, thread.GetId(), thread.GetResolvedAt() == ""

	doing, notice := "reopening the thread", "thread reopened"
	if settling {
		doing, notice = "settling the thread", "thread settled"
	}

	return r.writeCmd(s, h.beginWrite(doing), notice, func(ctx context.Context) error {
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

	file, ok := r.diff.View().GetPatchFileAt(r.diff.Cursor())
	if !ok {
		return nil
	}

	path := file.Path()
	viewed := !r.diff.View().IsViewed(path)

	// Where the cursor is standing is taken before the rows are built again.
	place, located := r.diff.View().GetPlace(r.diff.Cursor())
	r.diff.View().SetFileViewed(path, viewed)
	r.diff.Keep(place, located)

	doing, notice := "opening the file again", "file opened again"
	if viewed {
		doing, notice = "marking the file viewed", "file viewed"
		r.diff.MoveTo(r.diff.View().JumpFile(r.diff.Cursor(), 1), r.diffHeight(s))
	}

	r.scrollToCursor(s)

	agents, agentID := s.api.agents, r.agentID
	fingerprint := file.Fingerprint()

	return r.writeCmd(s, h.beginWrite(doing), notice, func(ctx context.Context) error {
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

	return r.quietWriteCmd(s, func(ctx context.Context) error {
		_, err := agents.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: id}))

		return err
	})
}

// openRelease asks what to send rather than sending.
func (r *reviewScreen) openRelease(s panelShared) tea.Cmd {
	r.threads.releasing = true

	return r.openInput(s, newReviewInputParams{
		Title:       "Anything about the work as a whole?",
		Description: reviewInputHint(formKeys().Editor, formKeys().Submit, "sends"),
		Placeholder: "optional",
	})
}

// updateRelease takes every key while the panel is up: the submit key sends, escape abandons,
// and everything else is a word being typed about the work.
func (r *reviewScreen) updateRelease(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	outcome := r.threads.editor.updateVim(key)
	if outcome != vim.Pass && outcome != vim.Leave {
		return nil
	}

	k := key.String()

	switch {
	case outcome == vim.Leave || isBound(k, formKeys().Back):
		r.threads.closeRelease()

		return nil
	case isBound(k, formKeys().Quit):
		return h.hide()
	case isBound(k, formKeys().Editor):
		return r.openMessageEditor(h, s)
	case isBound(k, formKeys().Submit):
		return r.send(h, s, false)
	case isBound(k, formKeys().SendNow):
		return r.send(h, s, true)
	}

	editor, cmd := r.threads.editor.update(key)
	r.threads.editor = editor

	return cmd
}

// send releases the batch with whatever was written about the work as a whole, reaching the
// agent as one file and one piece of mail.
func (r *reviewScreen) send(h host, s panelShared, interrupt bool) tea.Cmd {
	body := strings.TrimSpace(r.threads.editor.value())

	r.threads.closeRelease()

	agents, agentID := s.api.agents, r.agentID

	return r.sendCmd(h, s, "sending the review", func(ctx context.Context) (string, error) {
		resp, err := agents.ReleaseReview(ctx, connect.NewRequest(&agentv1.ReleaseReviewRequest{
			AgentId:   agentID,
			Interrupt: interrupt,
			Body:      body,
		}))
		if err != nil {
			return "", err
		}

		return releasedNotice(resp.Msg.GetComments(), interrupt), nil
	})
}

// closeRelease puts the release panel away, sent or not.
func (t *reviewThreads) closeRelease() {
	t.editor = reviewInput{}
	t.releasing = false
}

// releasedNotice says what went.
func releasedNotice(count int32, interrupt bool) string {
	sent := label.Counted(int(count), "comment") + " sent"
	if interrupt {
		sent += " and the agent stopped to read them"
	}

	return sent
}

// writeCmd runs one write on the review, landing the write that beginWrite counted out.
func (r reviewScreen) writeCmd(s panelShared, counted int, notice string, write func(context.Context) error) tea.Cmd {
	return r.writeMsgCmd(s, counted, func(ctx context.Context) (string, error) {
		return notice, write(ctx)
	})
}

// quietWriteCmd runs one write on the review that the panel does not count in flight.
func (r reviewScreen) quietWriteCmd(s panelShared, write func(context.Context) error) tea.Cmd {
	return r.writeCmd(s, 0, "", write)
}

// sendCmd is the write that releases a review.
func (r reviewScreen) sendCmd(h host, s panelShared, doing string, write func(context.Context) (string, error)) tea.Cmd {
	return r.writeMsgCmd(s, h.beginWrite(doing), write)
}

// writeMsgCmd runs one write, marking whose answer it is, and reads an archived agent's threads back.
func (r reviewScreen) writeMsgCmd(s panelShared, counted int, write func(context.Context) (string, error)) tea.Cmd {
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
			write:   counted,
		}
		if err != nil || live {
			return wrote
		}

		wrote.readBack = true
		wrote.threads, wrote.readErr = listReviewThreads(ctx, agents, agentID)

		return wrote
	}
}
