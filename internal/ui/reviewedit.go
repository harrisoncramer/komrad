package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"weave/internal/git"
	"weave/internal/review"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
)

// roundHarvestedMsg is what writing the reviewer's edits as threads did, carrying the review as
// it stands afterwards.
type roundHarvestedMsg struct {
	agentID string
	opened  int
	moved   int
	deleted int
	skipped int
	threads []*agentv1.ReviewThread
	err     error
}

// defaultEditor is what the file is opened in when nothing said otherwise.
const defaultEditor = "vi"

// roundReadyMsg says the round has been checked out somewhere the editor can open it, naming the
// snapshot it was put on rather than the one on screen.
type roundReadyMsg struct {
	agentID string
	dir     string
	sha     string
	// leftover is the checkout of an earlier round that was found holding work nobody carried, set
	// instead of a directory since nothing was checked out over the top of it.
	leftover string
	err      error
}

// roundEditedMsg is what the reviewer has written into the checkout, read back once the editor
// has gone and measured against the snapshot that checkout is standing on.
type roundEditedMsg struct {
	agentID string
	sha     string
	edits   []git.Edit
	err     error
}

// roundLeftoverMsg is a checkout found already standing beside the agent's worktree, holding work
// an earlier sitting wrote and nobody carried, named by the snapshot it stands on.
type roundLeftoverMsg struct {
	agentID string
	dir     string
	sha     string
	edits   []git.Edit
}

// editorShell is the shell line that runs the user's editor on a file, on one line of it.
func editorShell(editor, path string, line int) string {
	command := strings.TrimSpace(editor)
	if command == "" {
		command = defaultEditor
	}

	// The editor is left unquoted so an EDITOR of several words stays several words.
	return fmt.Sprintf("exec %s +%d %s", command, line, shellQuote(path))
}

// editorContent opens a file in the user's editor, on one line of it.
func editorContent(editor, dir, path string, line int) layout.Content {
	open := editorShell(editor, path, line)

	return layout.Content{
		Key:    fmt.Sprintf("edit:%s:%d", path, line),
		Dir:    dir,
		Render: func(layout.Target) ([]string, bool) { return []string{"sh", "-c", open}, true },
	}
}

// editReviewFile opens the round's own copy of the file under the cursor in a split under the
// diff, on the line the cursor is standing on, and reports whether anything was opened.
func (m model) editReviewFile() (tea.Model, tea.Cmd, bool) {
	path, line, ok := m.review.view.GetEditTarget(m.review.cursor)
	if !ok {
		m.review.notice = "that file was deleted, so there is nothing to open"

		return m, nil, false
	}

	target := fmt.Sprintf("%s:%d", path, line)
	if m.info.kind == infoEditFile && m.info.subject == target {
		next, cmd := m.closeReviewEdit()

		return next, cmd, false
	}

	// The checkout is asked for again whenever it stands on a snapshot other than the one being
	// read.
	if m.review.roundSha != m.review.round.GetSnapshotSha() {
		// A second press while the first checkout is still being made would run two of them at
		// the same path, where one reaps the directory the other is writing into.
		if m.review.materialising {
			return m, nil, false
		}

		m.review.materialising = true

		return m, m.materialiseRoundCmd(m.review.replacing != ""), true
	}

	if !m.claimInfo(infoEditFile, target) {
		return m, nil, false
	}

	m.frame.Show(layout.RegionInfo, editorContent(m.editor, m.review.roundDir, path, line))
	m.tmux.SelectPane(m.frame.Pane(layout.RegionInfo))

	return m, nil, true
}

// materialiseRoundCmd checks the round out beside the agent's worktree rather than in it, one
// directory per agent.
func (m model) materialiseRoundCmd(force bool) tea.Cmd {
	agentID, root, sha := m.review.agentID, m.review.root, m.review.round.GetSnapshotSha()
	at := filepath.Join(m.roundsDir, agentID)
	repo := m.git

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		// The disk is asked here rather than on the keypress.
		if !force && doesHoldLeftover(ctx, repo, at, sha) {
			return roundReadyMsg{agentID: agentID, leftover: at}
		}

		if err := repo.Materialise(ctx, root, sha, at); err != nil {
			return roundReadyMsg{agentID: agentID, err: err}
		}

		return roundReadyMsg{
			agentID: agentID,
			dir:     at,
			sha:     sha,
		}
	}
}

// doesHoldLeftover reports whether the checkout is an earlier round's with work still in it,
// which checking this round out over the top of it would destroy.
func doesHoldLeftover(ctx context.Context, repo uiGit, at, sha string) bool {
	head, ok := repo.WorktreeHead(ctx, at)
	if !ok || head == sha {
		return false
	}

	dirty, err := repo.IsDirty(ctx, at)

	return err != nil || dirty
}

// applyRoundReady opens the editor the checkout was asked for, on wherever the cursor has got to
// while it was being made.
func (m model) applyRoundReady(msg roundReadyMsg) (tea.Model, tea.Cmd) {
	if msg.agentID != m.review.agentID || m.review.view == nil {
		return m, nil
	}

	m.review.materialising = false

	if msg.leftover != "" {
		m.review.replacing = msg.leftover
		m.review.notice = "editing this round replaces the edit of an earlier one in " +
			msg.leftover + ", so press again to go on"

		return m, nil
	}

	if msg.err != nil {
		m.report("That round could not be checked out to edit", msg.err)

		return m, nil
	}

	m.review.replacing = ""
	m.review.roundDir = msg.dir
	m.review.roundSha = msg.sha

	next, cmd, _ := m.editReviewFile()

	return next, cmd
}

// reapRoundCmd takes the checkout away when the screen it was cut for closes, unless the reviewer
// wrote something into it that no comment has carried yet.
func (m model) reapRoundCmd(root, at string) tea.Cmd {
	if at == "" {
		return nil
	}

	repo := m.git

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		// Explainer: The whole tree is asked rather than the diff, since a file the reviewer wrote
		// from nothing is theirs too and no diff of the commit reports it. A checkout weave cannot
		// read is kept as well, since the answer it could not get is whether there was anything in
		// it worth keeping.
		if dirty, err := repo.IsDirty(ctx, at); err != nil || dirty {
			return nil
		}

		// A checkout that will not go is left where it is: the screen reading it has already
		// closed, and the next round to be edited replaces it rather than finding it.
		if err := repo.Reap(ctx, root, at); err != nil {
			return nil
		}

		return nil
	}
}

// findRoundLeftoverCmd looks for the checkout an earlier sitting left behind.
func (m model) findRoundLeftoverCmd() tea.Cmd {
	agentID := m.review.agentID
	at := filepath.Join(m.roundsDir, agentID)
	repo := m.git

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		sha, ok := repo.WorktreeHead(ctx, at)
		if !ok {
			return nil
		}

		edits, err := repo.Edits(ctx, at)
		if err != nil || len(edits) == 0 {
			return nil
		}

		return roundLeftoverMsg{
			agentID: agentID,
			dir:     at,
			sha:     sha,
			edits:   edits,
		}
	}
}

// applyRoundLeftover picks the checkout back up when it is standing on the round being read, and
// otherwise says it is there.
func (m *model) applyRoundLeftover(msg roundLeftoverMsg) {
	if msg.agentID != m.review.agentID || m.review.view == nil {
		return
	}

	// Anything the reader has done since the screen opened is theirs rather than the leftover's.
	if m.review.roundDir != "" {
		return
	}

	// Nothing takes this away on its own: it is unreadable against the round on screen, and the
	// only thing that ever clears it is the press that checks this round out over the top of it,
	// which asks the disk again rather than trusting what was found here.
	if msg.sha != m.review.round.GetSnapshotSha() {
		m.review.notice = "an edit of an earlier round is still in its checkout"

		return
	}

	m.review.roundDir = msg.dir
	m.review.roundSha = msg.sha
}

// readRoundEditsCmd asks the checkout what the reviewer wrote, which is the whole of it rather
// than only the file the editor was opened on.
func (m model) readRoundEditsCmd() tea.Cmd {
	agentID, at, sha := m.review.agentID, m.review.roundDir, m.review.roundSha
	if at == "" {
		return nil
	}

	repo := m.git

	return func() tea.Msg {
		edits, err := call(reviewTimeout, func(ctx context.Context) ([]git.Edit, error) {
			return repo.Edits(ctx, at)
		})

		return roundEditedMsg{
			agentID: agentID,
			sha:     sha,
			edits:   edits,
			err:     err,
		}
	}
}

// reconcileEditor catches the editor pane going away on its own and takes the window back for the
// diff it was covering.
func (m model) reconcileEditor() (model, tea.Cmd) {
	if !m.reconcileInfo() || m.screen != screenReview {
		return m, nil
	}

	return m, m.readRoundEditsCmd()
}

func (m model) closeReviewEdit() (tea.Model, tea.Cmd) {
	m = m.closeInfo()

	return m, m.readRoundEditsCmd()
}

// applyRoundEdited sends what the reviewer wrote to be written as threads, which is what makes an
// edit a comment without them pressing anything else.
func (m *model) applyRoundEdited(msg roundEditedMsg) tea.Cmd {
	if msg.agentID != m.review.agentID || m.review.view == nil {
		return nil
	}

	m.review.harvesting = false

	if msg.err != nil {
		m.report("What you wrote in the round could not be read back", msg.err)

		return nil
	}

	// The runs are numbered against the snapshot the checkout stands on, so a round that moved
	// while the editor was up leaves them naming lines of a file the diff no longer draws.
	if msg.sha != m.review.round.GetSnapshotSha() {
		m.review.notice = "the round moved on while you were editing, so what you wrote was not read"

		return nil
	}

	return m.harvestCmd(msg.edits)
}

// harvestCmd writes the runs the reviewer left in the checkout as threads on the round, anchored
// to the patch the line numbers and quoted lines come from.
func (m model) harvestCmd(edits []git.Edit) tea.Cmd {
	// An empty harvest is sent rather than skipped: a reviewer who has undone every edit is
	// retracting the comments those edits wrote, and only the server can see that.
	runs := review.RunsFrom(m.review.view, edits, m.roundFallback())

	agents, agentID := m.api.agents, m.review.agentID
	roundID, force := m.review.round.GetId(), m.review.oversized

	// The runs were anchored against the patch on screen, so the harvest says which commit that
	// measured their old side from rather than leaving the service to assume the round's base.
	base := m.review.view.GetBaseSHA()

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		resp, err := agents.HarvestRoundEdits(ctx, connect.NewRequest(&agentv1.HarvestRoundEditsRequest{
			AgentId: agentID,
			RoundId: roundID,
			Runs:    toHarvestedRuns(runs),
			BaseSha: base,
			Force:   force,
		}))
		if err != nil {
			return roundHarvestedMsg{agentID: agentID, err: err}
		}

		threads, err := listReviewThreads(ctx, agents, agentID)

		return roundHarvestedMsg{
			agentID: agentID,
			opened:  int(resp.Msg.GetOpened()),
			moved:   int(resp.Msg.GetMoved()),
			deleted: int(resp.Msg.GetDeleted()),
			skipped: len(resp.Msg.GetSkipped()),
			threads: threads,
			err:     err,
		}
	}
}

// roundFallback reads the round's own copy of a file the patch does not draw, which is how an
// edit made away from the diff still becomes a comment.
func (m model) roundFallback() func(string) ([]string, bool) {
	dir := m.review.roundDir
	if dir == "" {
		return nil
	}

	repo := m.git

	return func(path string) ([]string, bool) {
		lines, err := call(reviewTimeout, func(ctx context.Context) ([]string, error) {
			return repo.ReadFileLines(ctx, dir, "HEAD", path)
		})

		return lines, err == nil
	}
}

func toHarvestedRuns(runs []review.Run) []*agentv1.HarvestedRun {
	out := make([]*agentv1.HarvestedRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, &agentv1.HarvestedRun{
			FilePath:    run.FilePath,
			OldStart:    int32(run.OldStart),
			OldEnd:      int32(run.OldEnd),
			NewStart:    int32(run.NewStart),
			NewEnd:      int32(run.NewEnd),
			QuotedLines: run.QuotedLines,
			Lines:       run.Lines,
		})
	}

	return out
}

// applyRoundHarvested says what the harvest did.
func (m *model) applyRoundHarvested(msg roundHarvestedMsg) {
	if msg.agentID != m.review.agentID {
		return
	}

	if msg.err != nil {
		if isHarvestTooLarge(msg.err) {
			m.review.oversized = true
			m.review.notice = "that is a lot of changed lines for review comments, " +
				"press again to write them anyway"

			return
		}

		m.report("What you wrote could not be written as comments", msg.err)

		return
	}

	m.review.oversized = false
	m.review.notice = describeHarvest(msg)
	m.applyReviewThreads(msg.threads)
}

// describeHarvest names what changed, and says nothing when nothing did.
func describeHarvest(msg roundHarvestedMsg) string {
	var parts []string

	if msg.opened > 0 {
		parts = append(parts, counted(msg.opened, "comment")+" written from your edit")
	}

	if msg.moved > 0 {
		parts = append(parts, counted(msg.moved, "comment")+" rewritten")
	}

	if msg.deleted > 0 {
		parts = append(parts, counted(msg.deleted, "comment")+" taken back")
	}

	if msg.skipped > 0 {
		parts = append(parts, counted(msg.skipped, "edit")+" left alone over a comment already sent")
	}

	return strings.Join(parts, ", ")
}

// isHarvestTooLarge reports whether the server refused the harvest for its size, which is the one
// refusal the reviewer can answer by pressing again.
func isHarvestTooLarge(err error) bool {
	return strings.Contains(err.Error(), "too large to read as review comments")
}

func (m model) reviewEditorDesc() string {
	if m.info.kind == infoEditFile {
		return "close the editor"
	}

	return "open in your editor"
}
