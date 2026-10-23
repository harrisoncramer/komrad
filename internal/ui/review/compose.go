package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
	"komrad/internal/vim"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

// compose opens the editor on the line under the cursor, replying in the thread already there
// or opening a new one, writing prose alone rather than editing the snapshot.
func (r *reviewScreen) compose(in screen.Inputs) screen.Request {
	thread, replying := r.diff.threadAtCursor()

	// A thread whose code is gone has no line to anchor a new thread to, but replying to it is
	// what it is still on screen for.
	if _, ok := r.diff.View().GetAnchor(r.diff.Cursor()); !ok && !replying {
		return screen.NewNote(screen.Warning, "put the cursor on a line of the diff to comment on it")
	}

	r.threads.editing = ""
	if replying {
		r.threads.editing = thread.GetId()
	}

	r.threads.composing = true
	r.threads.retrying = nil
	r.threads.forge = nil

	return r.openInput(in, newReviewInputParams{
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
func (r *reviewScreen) openInput(in screen.Inputs, params newReviewInputParams) screen.Request {
	params.Width = reviewInputWidth(r.measurePopupInner(in))
	params.Keys = fixedKeys()
	params.Register = r.deps.register

	input, cmd := newReviewInput(params)
	r.threads.editor = input

	return screen.Cmd(cmd)
}

func (r *reviewScreen) updateComposer(in screen.Inputs, key tea.KeyPressMsg) screen.Request {
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
		return screen.Hide{}
	case keys.IsBound(k, fixedKeys().Editor):
		return r.openMessageEditor()
	case keys.IsBound(k, fixedKeys().Submit):
		return r.saveMessage(in)
	}

	editor, cmd := r.threads.editor.update(key)
	r.threads.editor = editor

	return screen.Cmd(cmd)
}

// messageEditedMsg is what the reviewer wrote in their own editor.
type messageEditedMsg struct {
	agentID string
	body    string
	err     error
}

// openMessageEditor hands what has been typed so far to the reviewer's editor.
func (r reviewScreen) openMessageEditor() screen.Request {
	agentID := r.agentID
	path := messageDraftPath(agentID)

	if err := os.WriteFile(path, []byte(r.threads.editor.value()), 0o600); err != nil {
		return screen.Report{Summary: "Your editor could not be opened on that message", Err: err}
	}

	return screen.Cmd(r.deps.editor.Edit(path, func(err error) tea.Msg {
		if err != nil {
			return messageEditedMsg{agentID: agentID, err: err}
		}

		body, err := os.ReadFile(path)

		return messageEditedMsg{
			agentID: agentID,
			body:    string(body),
			err:     err,
		}
	}))
}

// messageDraftPath is where the message is put for the editor to open.
func messageDraftPath(agentID string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("komrad-comment-%s-%d.md", agentID, os.Getpid()))
}

// applyMessageEdited puts what the editor wrote back into the composer.
func (r *reviewScreen) applyMessageEdited(in screen.Inputs, msg messageEditedMsg) screen.Request {
	if msg.agentID != r.agentID || !r.threads.composing {
		return nil
	}

	if msg.err != nil {
		return screen.Report{Summary: "What you wrote in your editor could not be read back", Err: msg.err}
	}

	title, said := r.threads.inputHead()

	return r.openInput(in, newReviewInputParams{
		Title:       title,
		Description: reviewInputHint(fixedKeys().Editor, fixedKeys().Submit, said),
		Body:        strings.TrimRight(msg.body, "\n"),
	})
}

func (r *reviewScreen) saveMessage(in screen.Inputs) screen.Request {
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
		return r.saveForgeReply(in, forge, body)
	}

	agents, threadID := r.deps.clients.Agents, r.threads.editing
	kept := reviewUnsent{editing: threadID, body: body}
	if threadID != "" {
		// Nothing is said back: the reply is rendered in the thread it went into.
		return r.writeMsg(in, "replying", &kept, func(ctx context.Context) (string, error) {
			_, err := agents.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{
				ThreadId: threadID,
				Body:     body,
			}))

			return "", err
		})
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

	return r.createThread(in, *place, kept)
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

// restoreMessage opens the composer again on a message the server refused.
func (r *reviewScreen) restoreMessage(in screen.Inputs, unsent reviewUnsent) screen.Request {
	if r.threads.composing {
		return nil
	}

	r.threads.editing = unsent.editing
	r.threads.composing = true
	r.threads.retrying = &unsent
	r.threads.forge = unsent.forge

	title, said := r.threads.inputHead()

	return r.openInput(in, newReviewInputParams{
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

func (r reviewScreen) createThread(in screen.Inputs, place reviewPlace, kept reviewUnsent) screen.Request {
	agents, taskID, body := r.deps.clients.Agents, r.taskID, kept.body
	anchor := place.anchor

	return r.writeMsg(in, "opening a thread", &kept, func(ctx context.Context) (string, error) {
		_, err := agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
			Task:        &agentv1.CreateReviewThreadRequest_TaskId{TaskId: taskID},
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

		return "thread opened", err
	})
}

// resolveThread settles the thread the cursor is standing on, or reopens one already settled.
func (r reviewScreen) resolveThread(in screen.Inputs) screen.Request {
	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	agents, id, settling := r.deps.clients.Agents, thread.GetId(), thread.GetResolvedAt() == nil

	doing, notice := "reopening the thread", "thread reopened"
	if settling {
		doing, notice = "settling the thread", "thread settled"
	}

	return r.write(in, doing, notice, func(ctx context.Context) error {
		_, err := agents.SetReviewThreadResolved(ctx, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{
			Id:       id,
			Resolved: settling,
		}))

		return err
	})
}

// toggleViewed folds the file the cursor is in away once it has been read, or opens it again,
// stepping on to the next file when it has just been folded.
func (r *reviewScreen) toggleViewed(in screen.Inputs) screen.Request {
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
		r.diff.MoveTo(r.diff.View().JumpFile(r.diff.Cursor(), 1), r.diffWindow(in))
	}

	r.scrollToCursor(in)

	agents, taskID := r.deps.clients.Agents, r.taskID
	fingerprint := file.Fingerprint()

	return r.write(in, doing, notice, func(ctx context.Context) error {
		_, err := agents.SetReviewFileViewed(ctx, connect.NewRequest(&agentv1.SetReviewFileViewedRequest{
			Task:        &agentv1.SetReviewFileViewedRequest_TaskId{TaskId: taskID},
			FilePath:    path,
			Fingerprint: fingerprint,
			Viewed:      viewed,
		}))

		return err
	})
}

// markThreadRead claims the thread the cursor came to rest on and only that one.
func (r reviewScreen) markThreadRead(in screen.Inputs) screen.Request {
	thread, ok := r.diff.threadAtCursor()
	if !ok || !thread.GetUnreadByHuman() {
		return nil
	}

	agents, id := r.deps.clients.Agents, thread.GetId()

	return r.quietWrite(in, func(ctx context.Context) error {
		_, err := agents.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: id}))

		return err
	})
}

// write runs one write on the review, saying notice once it lands.
func (r reviewScreen) write(in screen.Inputs, doing, notice string, write func(context.Context) error) screen.Request {
	return r.writeMsg(in, doing, nil, func(ctx context.Context) (string, error) {
		return notice, write(ctx)
	})
}

// quietWrite runs one write on the review that the panel does not count in flight.
func (r reviewScreen) quietWrite(in screen.Inputs, write func(context.Context) error) screen.Request {
	return r.write(in, "", "", write)
}

// writeMsg runs one write, marking whose answer it is, handing back the message a refused write
// carried, and reading an archived agent's threads back.
func (r reviewScreen) writeMsg(in screen.Inputs, doing string, unsent *reviewUnsent, write func(context.Context) (string, error)) screen.Request {
	agents, agentID, taskID := r.deps.clients.Agents, r.agentID, r.taskID
	_, live := r.agentRow(in)

	return screen.Write{
		Doing: doing,
		Send: func() screen.Landed {
			ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
			defer cancel()

			notice, err := write(ctx)
			wrote := reviewWroteMsg{
				agentID: agentID,
				notice:  notice,
				err:     err,
			}

			switch {
			case err != nil && !errors.Is(err, errForgeRefused):
				wrote.unsent = unsent
			case err == nil && !live:
				wrote.readBack = true
				wrote.threads, wrote.readErr = listReviewThreads(ctx, agents, taskID)
			}

			return screen.Landed{Then: wrote}
		},
	}
}

// inputHead is the composer's title and what its submit key is said to do.
func (t reviewThreads) inputHead() (string, string) {
	if t.forge != nil {
		return t.forge.title, "post"
	}

	return reviewInputTitle(t.editing != ""), "save"
}
