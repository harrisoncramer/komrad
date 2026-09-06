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

// roundReadyMsg says the round has been checked out somewhere the editor can open it, naming the
// snapshot it was put on rather than the one on screen, which may have moved while it was made.
type roundReadyMsg struct {
	agentID string
	dir     string
	sha     string
	err     error
}

// roundEditedMsg is what the reviewer has written into the checkout, read back once the editor
// that was writing it has gone, against the snapshot that checkout is standing on.
type roundEditedMsg struct {
	agentID string
	sha     string
	edits   []git.Edit
	err     error
}

// roundLeftoverMsg is a checkout found already standing beside the agent's worktree, holding work
// an earlier sitting wrote and nobody carried, named by the snapshot it stands on since that is
// what says whether it belongs to the round on screen.
type roundLeftoverMsg struct {
	agentID string
	dir     string
	sha     string
	edits   []git.Edit
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

	// The checkout is asked for again whenever it stands on a snapshot other than the one being
	// read, since opening the editor on the previous round is the whole bug this key was changed
	// to close.
	if m.review.roundSha != m.review.round.GetSnapshotSha() {
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

		return roundReadyMsg{agentID: agentID, dir: at, sha: sha}
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
	m.review.roundSha = msg.sha

	return m.editReviewFile()
}

// reapRoundCmd takes the checkout away when the screen it was cut for closes, unless the reviewer
// wrote something into it that no comment has carried yet, since leaving a screen is not an answer
// to what they typed and the checkout is the only place that text lives.
func (m model) reapRoundCmd(root, at string) tea.Cmd {
	if at == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		// A checkout weave cannot read is kept as well, since the answer it could not get is
		// whether there was anything in it worth keeping.
		if edits, err := git.Edits(ctx, at); err != nil || len(edits) > 0 {
			return nil
		}

		// A checkout that will not go is left where it is: the screen reading it has already
		// closed, and the next round to be edited replaces it rather than finding it.
		if err := git.Reap(ctx, root, at); err != nil {
			return nil
		}

		return nil
	}
}

// findRoundLeftoverCmd looks for the checkout an earlier sitting left behind, since the edits in it
// were never carried anywhere else and the reader would otherwise never be offered them again.
func (m model) findRoundLeftoverCmd() tea.Cmd {
	agentID := m.review.agentID
	at := filepath.Join(m.roundsDir, agentID)

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		sha, ok := git.WorktreeHead(ctx, at)
		if !ok {
			return nil
		}

		edits, err := git.Edits(ctx, at)
		if err != nil || len(edits) == 0 {
			return nil
		}

		return roundLeftoverMsg{agentID: agentID, dir: at, sha: sha, edits: edits}
	}
}

// applyRoundLeftover picks the checkout back up when it is standing on the round being read, and
// otherwise says it is there, since the next edit checks this round out over the top of it.
func (m *model) applyRoundLeftover(msg roundLeftoverMsg) {
	if msg.agentID != m.review.agentID || m.review.view == nil {
		return
	}

	// Anything the reader has done since the screen opened is theirs rather than the leftover's.
	if m.review.roundDir != "" || len(m.review.suggestions) > 0 {
		return
	}

	if msg.sha != m.review.round.GetSnapshotSha() {
		m.review.notice = "an edit of an earlier round is still in its checkout, and editing this one replaces it"

		return
	}

	m.review.roundDir = msg.dir
	m.review.roundSha = msg.sha
	m.review.suggestions, m.review.adrift = m.holdSuggestions(msg.edits)
}

// readRoundEditsCmd asks the checkout what the reviewer wrote, which is the whole of it rather than
// the file the editor was opened on, since an editor is free to visit another file.
func (m model) readRoundEditsCmd() tea.Cmd {
	agentID, at, sha := m.review.agentID, m.review.roundDir, m.review.roundSha
	if at == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		edits, err := git.Edits(ctx, at)

		return roundEditedMsg{
			agentID: agentID,
			sha:     sha,
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

	// The runs are numbered against the snapshot the checkout stands on, so a round that moved
	// while the editor was up leaves them naming lines of a file the diff no longer draws.
	if msg.sha != m.review.round.GetSnapshotSha() {
		m.review.notice = "the round moved on while you were editing, so what you wrote was not read"

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
