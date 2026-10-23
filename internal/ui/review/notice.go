package review

import (
	"slices"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

// notices is what the review has raised on the messages stack.
type notices struct {
	// raised is every key raised this visit and not since lowered, so a dismissed notice stays down.
	raised []string
	// moved is the key of the notice about the agent having written past the diff on screen.
	moved string
}

// raise pushes a notice once per visit.
func (n *notices) raise(held screen.Note) screen.Request {
	if slices.Contains(n.raised, held.Key) {
		return nil
	}

	n.raised = append(n.raised, held.Key)

	return held
}

// lower takes a notice down once it stops being true, so it can rise again.
func (n *notices) lower(key string) screen.Request {
	n.raised = slices.DeleteFunc(n.raised, func(raised string) bool { return raised == key })

	return screen.Unnote{Key: key}
}

// followNotices raises what the review has to say about the agent and lowers what is no longer true.
func (r *Screen) followNotices(in screen.Inputs) screen.Request {
	if r.diff.View() == nil || r.diff.View().Len() == 0 || !in.Up {
		return nil
	}

	var requests screen.Batch

	archived := "archived:" + r.agentID
	if r.isArchived(in) {
		requests = append(requests, r.notices.raise(screen.Note{
			Kind:    screen.Warning,
			Key:     archived,
			Summary: "this agent is archived",
			Detail:  "restore it before writing to it",
		}))
	} else {
		requests = append(requests, r.notices.lower(archived))
	}

	moved := r.movedNotice(in)
	if moved.Key != r.notices.moved {
		requests = append(requests, r.notices.lower(r.notices.moved))
		r.notices.moved = moved.Key
	}

	if moved.Key != "" {
		requests = append(requests, r.notices.raise(moved))
	}

	return requests
}

// movedNotice is what the reader is told about the agent having written past the diff on screen.
func (r Screen) movedNotice(in screen.Inputs) screen.Note {
	moved := r.findMovedSnapshot(in)
	if moved == "" || r.edit.atEditor {
		return screen.Note{}
	}

	return screen.Note{
		Kind:    screen.Warning,
		Key:     "moved:" + r.agentID + ":" + moved,
		Summary: "the worktree has moved",
		Detail:  keys.MenuChordLabel(r.deps.Keys, r.deps.Keys.Review.Reload) + " to catch up",
	}
}
