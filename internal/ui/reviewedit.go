package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"weave/internal/git"
	"weave/internal/review"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
)

// defaultEditor is what the file is opened in when nothing said otherwise, which is what the
// environment falls back to as well.
const defaultEditor = "vi"

// roundReadyMsg says the round has been checked out somewhere the editor can open it.
type roundReadyMsg struct {
	agentID string
	dir     string
	err     error
}

// roundEditedMsg is what the reviewer has written into the checkout, read back once the editor
// that was writing it has gone.
type roundEditedMsg struct {
	agentID string
	edits   []git.Edit
	err     error
}

// editorContent opens a file in the user's editor, on one line of it.
func editorContent(editor, dir, path string, line int) layout.Content {
	command := strings.TrimSpace(editor)
	if command == "" {
		command = defaultEditor
	}

	// The editor is left unquoted so an EDITOR of several words stays several words.
	open := fmt.Sprintf("exec %s +%d %s", command, line, shellQuote(path))

	return layout.Content{
		Key:    fmt.Sprintf("edit:%s:%d", path, line),
		Dir:    dir,
		Render: func(layout.Target) ([]string, bool) { return []string{"sh", "-c", open}, true },
	}
}

// editReviewFile opens the round's own copy of the file under the cursor in a split under the
// diff, so that what the reviewer types is invisible to the agent still writing to the worktree,
// and the line the cursor is standing on is a line of the file being opened.
func (m model) editReviewFile() (tea.Model, tea.Cmd) {
	path, line, ok := m.review.view.GetEditTarget(m.review.cursor)
	if !ok {
		m.review.notice = "that file was deleted, so there is nothing to open"

		return m, nil
	}

	target := fmt.Sprintf("%s:%d", path, line)
	if m.info.kind == infoEditFile && m.info.subject == target {
		return m.closeReviewEdit()
	}

	if m.review.roundDir == "" {
		// A second press while the first checkout is still being made would run two of them at
		// the same path, where one reaps the directory the other is writing into.
		if m.review.materialising {
			return m, nil
		}

		m.review.materialising = true

		return m, m.materialiseRoundCmd()
	}

	if !m.claimInfo(infoEditFile, target) {
		return m, nil
	}

	m.frame.Show(layout.RegionInfo, editorContent(m.editor, m.review.roundDir, path, line))
	m.remeasureReview()
	m.tmux.SelectPane(m.frame.Pane(layout.RegionInfo))

	return m, nil
}

// materialiseRoundCmd checks the round out beside the agent's worktree rather than in it, one
// directory per agent, since a reader opens many files of one round and rarely two rounds at once.
func (m model) materialiseRoundCmd() tea.Cmd {
	agentID, root, sha := m.review.agentID, m.review.root, m.review.round.GetSnapshotSha()
	at := filepath.Join(m.roundsDir, agentID)

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		if err := git.Materialise(ctx, root, sha, at); err != nil {
			return roundReadyMsg{agentID: agentID, err: err}
		}

		return roundReadyMsg{agentID: agentID, dir: at}
	}
}

// applyRoundReady opens the editor the checkout was asked for, on wherever the cursor has got to
// while it was being made.
func (m model) applyRoundReady(msg roundReadyMsg) (tea.Model, tea.Cmd) {
	if msg.agentID != m.review.agentID || m.review.view == nil {
		return m, nil
	}

	m.review.materialising = false

	if msg.err != nil {
		m.review.notice = "that round could not be checked out to edit"

		return m, nil
	}

	m.review.roundDir = msg.dir

	return m.editReviewFile()
}

// reapRoundCmd takes the checkout away when the screen it was cut for closes, which is the only
// moment weave knows nobody is reading it.
func (m model) reapRoundCmd(root, at string) tea.Cmd {
	if at == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		// A checkout that will not go is left where it is: the screen reading it has already
		// closed, and the next round to be edited replaces it rather than finding it.
		if err := git.Reap(ctx, root, at); err != nil {
			return nil
		}

		return nil
	}
}

// readRoundEditsCmd asks the checkout what the reviewer wrote, which is the whole of it rather than
// the file the editor was opened on, since an editor is free to visit another file.
func (m model) readRoundEditsCmd() tea.Cmd {
	agentID, at := m.review.agentID, m.review.roundDir
	if at == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		edits, err := git.Edits(ctx, at)

		return roundEditedMsg{
			agentID: agentID,
			edits:   edits,
			err:     err,
		}
	}
}

// reconcileEditor catches the editor pane going away on its own and puts the diff behind it back
// on screen at the size the stage is left with.
func (m model) reconcileEditor() (model, tea.Cmd) {
	if !m.reconcileInfo() || m.screen != screenReview {
		return m, nil
	}

	m.remeasureReview()

	return m, m.readRoundEditsCmd()
}

func (m model) closeReviewEdit() (tea.Model, tea.Cmd) {
	m = m.closeInfo()
	m.remeasureReview()

	return m, m.readRoundEditsCmd()
}

// applyRoundEdited holds every run the reviewer wrote as a suggestion of its own, so that two
// changes to one file are two proposals rather than one span swallowing the code between them.
func (m *model) applyRoundEdited(msg roundEditedMsg) {
	if msg.agentID != m.review.agentID || m.review.view == nil {
		return
	}

	if msg.err != nil {
		m.review.notice = "what you wrote in the round could not be read back"

		return
	}

	m.review.suggestions, m.review.adrift = m.holdSuggestions(msg.edits)
}

// holdSuggestions turns the runs into proposals, dropping the ones already sent or turned down and
// answering separately with the ones the diff has no line for.
func (m model) holdSuggestions(edits []git.Edit) (held []pendingSuggestion, adrift []review.Suggestion) {
	for _, edit := range edits {
		source, ok := m.review.view.GetSource(edit.Path)
		if !ok {
			continue
		}

		suggestion, ok := review.Suggest(edit, review.SplitSource(source))
		if !ok || m.review.settled[suggestion.Key()] {
			continue
		}

		anchor, ok := m.review.view.GetRangeAnchor(edit.Path, suggestion.NewStart, suggestion.NewEnd)
		if !ok {
			adrift = append(adrift, suggestion)

			continue
		}

		held = append(held, pendingSuggestion{anchor: *anchor, lines: suggestion.Lines})
	}

	return held, adrift
}

// remeasureReview draws the diff again at the size the stage is left with, since the drawer is
// handed exactly one screenful and a stale size is rows the pane cannot show.
func (m *model) remeasureReview() {
	m.frame.Claimed(layout.RegionStage)
	m.stageReview()
}

func (m model) reviewEditorDesc() string {
	if m.info.kind == infoEditFile {
		return "close the editor"
	}

	return "open in your editor"
}
