package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/keys"
	"komrad/internal/vim"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

// compose opens the editor on the line under the cursor, replying in the thread already there
// or opening a new one, writing prose alone rather than editing the snapshot.
func (r *reviewScreen) compose(m *model) tea.Cmd {
	thread, replying := r.diff.threadAtCursor()

	// A thread whose code is gone has no line to anchor a new thread to, but replying to it is
	// what it is still on screen for.
	if _, ok := r.diff.View().GetAnchor(r.diff.Cursor()); !ok && !replying {
		r.refuseLine(m)

		return nil
	}

	r.threads.editing = ""
	if replying {
		r.threads.editing = thread.GetId()
	}

	r.threads.composing = true
	r.threads.retrying = nil
	r.threads.forge = nil

	return r.openInput(m, newReviewInputParams{
		Title:       reviewInputTitle(replying),
		Description: reviewInputHint(fixedKeys().Editor, fixedKeys().Submit, "save"),
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
	return keys.Label(editor) + " opens $EDITOR; " + keys.Label(submit) + " " + said + "; " +
		keys.Label(fixedKeys().Back) + " cancels"
}

// openInput puts a field in the popup, sized to the room inside it.
func (r *reviewScreen) openInput(m *model, params newReviewInputParams) tea.Cmd {
	params.Width = reviewInputWidth(r.measurePopupInner(m))
	params.Keys = fixedKeys()
	params.Register = m.register

	input, cmd := newReviewInput(params)
	r.threads.editor = input

	return cmd
}

// refuseLine says why nothing opened on a row that is not a line of code.
func (r *reviewScreen) refuseLine(m *model) {
	m.note(messageWarning, "put the cursor on a line of the diff to comment on it")
}

func (r *reviewScreen) updateComposer(m *model, key tea.KeyPressMsg) tea.Cmd {
	outcome := r.threads.editor.updateVim(key)
	if outcome != vim.Pass && outcome != vim.Leave {
		return nil
	}

	k := key.String()

	// Every other key is a character being typed into the message, so the keys that leave it, and
	// the one that opens it in an editor, come from the form keymap.
	switch {
	case outcome == vim.Leave || keys.IsBound(k, fixedKeys().Back):
		r.threads.cancelMessage()

		return nil
	case keys.IsBound(k, fixedKeys().Quit):
		return m.hide()
	case keys.IsBound(k, fixedKeys().Editor):
		return r.openMessageEditor(m)
	case keys.IsBound(k, fixedKeys().Submit):
		return r.saveMessage(m)
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
func (r reviewScreen) openMessageEditor(m *model) tea.Cmd {
	agentID := r.agentID
	path := messageDraftPath(agentID)

	if err := os.WriteFile(path, []byte(r.threads.editor.value()), 0o600); err != nil {
		m.report("Your editor could not be opened on that message", err)

		return nil
	}

	// The message is one file with nothing above it, so the editor opens on its first line.
	//nolint:noctx // the editor runs until the reviewer closes it, so it takes no deadline
	open := exec.Command("sh", "-c", editorShell(m.settings.Editor, path, 1))

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
func (r *reviewScreen) applyMessageEdited(m *model, msg messageEditedMsg) tea.Cmd {
	if msg.agentID != r.agentID || !r.threads.composing {
		return nil
	}

	if msg.err != nil {
		m.report("What you wrote in your editor could not be read back", msg.err)

		return nil
	}

	title, said := r.threads.inputHead()

	return r.openInput(m, newReviewInputParams{
		Title:       title,
		Description: reviewInputHint(fixedKeys().Editor, fixedKeys().Submit, said),
		Body:        strings.TrimRight(msg.body, "\n"),
	})
}

func (r *reviewScreen) saveMessage(m *model) tea.Cmd {
	body := strings.TrimSpace(r.threads.editor.value())
	if body == "" {
		r.threads.cancelMessage()

		return nil
	}

	retrying := r.threads.retrying
	forge := r.threads.forge
	r.threads.editor = reviewInput{}
	r.threads.composing = false
	r.threads.retrying = nil
	r.threads.forge = nil

	if forge != nil {
		return r.saveForgeReply(m, forge, body)
	}

	agents, threadID := m.clients().agents, r.threads.editing
	kept := reviewUnsent{editing: threadID, body: body}
	if threadID != "" {
		// Nothing is said back: the reply is rendered in the thread it went into.
		return keepUnsent(kept, r.writeCmd(m, m.beginWrite("replying"), "", func(ctx context.Context) error {
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
			// thread records that rather than leaving a later reader to assume the snapshot's own base.
			base: r.diff.View().GetBaseSHA(),
			// The new side names the snapshot it was read from, which the server compares against
			// the snapshot it would file the thread under rather than trusting the numbers.
			snapshot: r.diff.View().GetSnapshotSHA(),
		}
		if place.snapshot == "" {
			place.snapshot = r.snapshot.GetSha()
		}
	}

	kept.place = place

	return keepUnsent(kept, r.createThreadCmd(m, *place, body))
}

// reviewUnsent is a message on its way to the server, held until the write carrying it lands.
type reviewUnsent struct {
	editing string
	body    string
	// place is where a new thread goes, nil for a reply.
	place *reviewPlace
	// forge is the reply to the forge the message was, nil for one to the agent.
	forge *reviewForgeCompose
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
		if !ok || wrote.err == nil || errors.Is(wrote.err, errForgeRefused) {
			return msg
		}

		wrote.unsent = &kept

		return wrote
	}
}

// restoreMessage opens the composer again on a message the server refused.
func (r *reviewScreen) restoreMessage(m *model, unsent reviewUnsent) tea.Cmd {
	if r.threads.composing {
		return nil
	}

	r.threads.editing = unsent.editing
	r.threads.composing = true
	r.threads.retrying = &unsent
	r.threads.forge = unsent.forge

	title, said := r.threads.inputHead()

	return r.openInput(m, newReviewInputParams{
		Title:       title,
		Description: reviewInputHint(fixedKeys().Editor, fixedKeys().Submit, said),
		Body:        unsent.body,
	})
}

// cancelMessage puts the composer away, abandoning nothing but the words typed into it.
func (t *reviewThreads) cancelMessage() {
	t.editor = reviewInput{}
	t.composing = false
	t.retrying = nil
	t.forge = nil
}

func (r reviewScreen) createThreadCmd(m *model, place reviewPlace, body string) tea.Cmd {
	agents, taskID := m.clients().agents, r.taskID
	anchor := place.anchor

	return r.writeCmd(m, m.beginWrite("opening a thread"), "thread opened", func(ctx context.Context) error {
		_, err := agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
			Task:        &taskID,
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
func (r reviewScreen) resolveThread(m *model) tea.Cmd {
	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	agents, id, settling := m.clients().agents, thread.GetId(), thread.GetResolvedAt() == ""

	doing, notice := "reopening the thread", "thread reopened"
	if settling {
		doing, notice = "settling the thread", "thread settled"
	}

	return r.writeCmd(m, m.beginWrite(doing), notice, func(ctx context.Context) error {
		_, err := agents.SetReviewThreadResolved(ctx, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{
			Id:       id,
			Resolved: settling,
		}))

		return err
	})
}

// toggleViewed folds the file the cursor is in away once it has been read, or opens it again,
// stepping on to the next file when it has just been folded.
func (r *reviewScreen) toggleViewed(m *model) tea.Cmd {
	if !r.diff.spec.isWholeChange() {
		return nil
	}

	file, ok := r.diff.View().GetPatchFileAt(r.diff.Cursor())
	if !ok {
		return nil
	}

	path := file.Path()
	viewed := !r.diff.View().IsViewed(path)

	// The cursor's place is read before the rows are built again.
	place, located := r.diff.View().GetPlace(r.diff.Cursor())
	r.diff.View().SetFileViewed(path, viewed)
	r.diff.Keep(place, located)

	doing, notice := "opening the file again", "file opened again"
	if viewed {
		doing, notice = "marking the file viewed", "file viewed"
		r.diff.MoveTo(r.diff.View().JumpFile(r.diff.Cursor(), 1), r.diffWindow(m))
	}

	r.scrollToCursor(m)

	agents, taskID := m.clients().agents, r.taskID
	fingerprint := file.Fingerprint()

	return r.writeCmd(m, m.beginWrite(doing), notice, func(ctx context.Context) error {
		_, err := agents.SetReviewFileViewed(ctx, connect.NewRequest(&agentv1.SetReviewFileViewedRequest{
			Task:        taskID,
			FilePath:    path,
			Fingerprint: fingerprint,
			Viewed:      viewed,
		}))

		return err
	})
}

// markThreadReadCmd claims the thread the cursor came to rest on and only that one.
func (r reviewScreen) markThreadReadCmd(m *model) tea.Cmd {
	thread, ok := r.diff.threadAtCursor()
	if !ok || !thread.GetUnreadByHuman() {
		return nil
	}

	agents, id := m.clients().agents, thread.GetId()

	return r.quietWriteCmd(m, func(ctx context.Context) error {
		_, err := agents.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: id}))

		return err
	})
}

// writeCmd runs one write on the review, landing the write that beginWrite counted out.
func (r reviewScreen) writeCmd(m *model, counted int, notice string, write func(context.Context) error) tea.Cmd {
	return r.writeMsgCmd(m, counted, func(ctx context.Context) (string, error) {
		return notice, write(ctx)
	})
}

// quietWriteCmd runs one write on the review that the panel does not count in flight.
func (r reviewScreen) quietWriteCmd(m *model, write func(context.Context) error) tea.Cmd {
	return r.writeCmd(m, 0, "", write)
}

// writeMsgCmd runs one write, marking whose answer it is, and reads an archived agent's threads back.
func (r reviewScreen) writeMsgCmd(m *model, counted int, write func(context.Context) (string, error)) tea.Cmd {
	agents, agentID, taskID := m.api.reads().agents, r.agentID, r.taskID
	_, live := r.agentRow(m)

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
		wrote.threads, wrote.readErr = listReviewThreads(ctx, agents, taskID)

		return wrote
	}
}

// inputHead is the composer's title and what its submit key is said to do.
func (t reviewThreads) inputHead() (string, string) {
	if t.forge != nil {
		return t.forge.title, "post"
	}

	return reviewInputTitle(t.editing != ""), "save"
}
