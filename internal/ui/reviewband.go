package ui

import (
	"context"
	"fmt"
	"strings"

	"komrad/internal/git"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/lib/dates"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2/ansi"
)

// reviewScopeOnTone is the half of the scope switch that is in force.
const reviewScopeOnTone = paint.TonePrimary

const reviewFrozenTone = paint.ToneWarning

// bandView is what stands over the diff: whose work is being read, the round it is frozen on, and
// the pattern it is searched for.
func (r reviewScreen) bandView(m *model) string {
	return paint.Panel.Title("Review"+separated+r.agentName) + "\n" +
		r.frozenView(m) +
		r.diff.filter.view(m.glyphs) + diffMatchCount(&r.diff.Pane, r.diff.filter)
}

func (r reviewScreen) frozenView(m *model) string {
	if !r.isFrozen() {
		return ""
	}

	said := fmt.Sprintf("frozen on round %d (%s", r.round.GetOrdinal(), git.ShortSHA(r.round.GetSnapshotSha()))
	if at := label.Friendly(dates.Parse(r.round.GetCreatedAt())); at != "" {
		said += ", taken " + at
	}
	said += ") until you send your review"

	style := dimStyle()
	if behind := r.behindSaid(m); behind != "" {
		said += separated + behind + separated + m.menuChordLabel(m.keys.Review.Reload) + " shows the current diff"
		style = paint.Panel.Style(reviewFrozenTone)
	}

	var out strings.Builder
	for _, row := range paint.WrapRows(said, r.bandWidth(m)) {
		out.WriteString(style.Render(row) + "\n")
	}

	return out.String()
}

func (r reviewScreen) isFrozen() bool {
	return r.diff.pinned && r.round != nil && r.diff.spec.answer == nil && r.diff.spec.origin == nil
}

func (r reviewScreen) behindSaid(m *model) string {
	newest := r.round
	if len(r.diff.rounds) > 0 {
		newest = r.diff.rounds[len(r.diff.rounds)-1]
	}

	rounds := int(newest.GetOrdinal() - r.round.GetOrdinal())
	moved := r.isCheckoutMoved(m)

	var parts []string
	switch {
	case rounds > 0 && moved:
		parts = append(parts, label.Counted(rounds, "round")+" behind, with more written since")
	case rounds > 0:
		parts = append(parts, label.Counted(rounds, "round")+" behind")
	case moved:
		parts = append(parts, "the worktree has moved since")
	}

	if newest.GetBaseSha() != r.round.GetBaseSha() {
		parts = append(parts, fmt.Sprintf("rebased since, from %s onto %s",
			git.ShortSHA(r.round.GetBaseSha()), git.ShortSHA(newest.GetBaseSha())))
	}

	return strings.Join(parts, separated)
}

func (r reviewScreen) isCheckoutMoved(m *model) bool {
	item, ok := r.agentRow(m)

	return ok && m.listing.tasks.findForAgent(item.Agent).MovedSinceRound != ""
}

func (r reviewScreen) bandWidth(m *model) int {
	if split, ok := m.columnGeometry(); ok {
		return split.column
	}

	return lineWidthOf(m.paneWidth())
}

// reviewColumnStyle is the markdown a notice standing in for the patch is rendered in.
func reviewColumnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}

type reviewRoundsReadMsg struct {
	agentID string
	rounds  []*agentv1.ReviewRound
}

func (r *reviewScreen) followRounds(m *model) tea.Cmd {
	if !r.isFrozen() || r.loading {
		return nil
	}

	item, ok := r.agentRow(m)
	if !ok {
		return nil
	}

	moved := m.listing.tasks.findForAgent(item.Agent).MovedSinceRound
	if moved == r.diff.roundsMove {
		return nil
	}
	r.diff.roundsMove = moved

	agents, agentID, taskID := m.api.reads().agents, r.agentID, r.taskID

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		rounds, err := listReviewRounds(ctx, agents, taskID)
		if err != nil {
			return nil
		}

		return reviewRoundsReadMsg{agentID: agentID, rounds: rounds}
	}
}

func (r *reviewScreen) applyRoundsRead(msg reviewRoundsReadMsg) {
	if msg.agentID == r.agentID && len(msg.rounds) >= len(r.diff.rounds) {
		r.diff.rounds = msg.rounds
	}
}
