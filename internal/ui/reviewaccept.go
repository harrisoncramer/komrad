package ui

import (
	"context"
	"slices"

	"komrad/internal/assemble"
	"komrad/internal/ui/diffview"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

// reviewAccept is a forge thread being accepted into the reader's review.
type reviewAccept struct {
	offerID string
	title   string
	place   reviewPlace
}

// acceptableOffer is the forge thread the accept key copies from at the cursor.
func (r reviewScreen) acceptableOffer(m *model) (*pullrequestv1.ForgeThread, bool) {
	threads := m.listing.world.ReviewThreads

	if thread, ok := r.diff.threadAtCursor(); ok {
		offers := r.listOffers(m)
		at := slices.IndexFunc(offers, func(offer *pullrequestv1.ForgeThread) bool {
			return offer.GetAcceptedThreadId() == thread.GetId()
		})
		if at < 0 || assemble.StateOfOffer(threads, offers[at]) != assemble.OfferChanged {
			return nil, false
		}

		return offers[at], true
	}

	offer, ok := r.offerAtCursor(m)
	if !ok || assemble.StateOfOffer(threads, offer) == assemble.OfferAccepted {
		return nil, false
	}

	return offer, true
}

func (r reviewScreen) doesAcceptOffer(m *model) bool {
	_, ok := r.acceptableOffer(m)

	return ok
}

// acceptOfferDesc names what the accept key does to the forge thread under the cursor.
func (r reviewScreen) acceptOfferDesc(m *model) string {
	if offer, ok := r.acceptableOffer(m); ok && assemble.StateOfOffer(m.listing.world.ReviewThreads, offer) == assemble.OfferChanged {
		return "accept what changed"
	}

	return "accept the forge comment"
}

// acceptOffer opens the composer under the forge thread at the cursor, for a note to send with it.
func (r *reviewScreen) acceptOffer(m *model) tea.Cmd {
	offer, ok := r.acceptableOffer(m)
	if !ok {
		return nil
	}

	place, ok := r.acceptPlace(m, offer)
	if !ok {
		m.note(messageWarning, "only a forge comment on a line of this diff can be accepted")

		return nil
	}

	r.threads.editing = ""
	r.threads.composing = true
	r.threads.retrying = nil
	r.threads.accepting = &reviewAccept{
		offerID: offer.GetId(),
		title:   acceptTitle(offer),
		place:   place,
	}

	title, said := r.threads.inputHead()

	return r.openInput(m, newReviewInputParams{
		Title:       title,
		Description: reviewInputHint(formKeys().Editor, formKeys().Submit, said),
		Placeholder: "anything to add for the agent? optional",
	})
}

// acceptPlace is the lines a forge thread is pinned to as the diff draws it, or the thread it was
// accepted into before.
func (r reviewScreen) acceptPlace(m *model, offer *pullrequestv1.ForgeThread) (reviewPlace, bool) {
	if thread, ok := m.listing.world.ReviewThreads[offer.GetAcceptedThreadId()]; ok {
		return reviewPlace{anchor: anchorOfThread(thread), base: thread.GetBaseSha()}, true
	}

	view := r.diff.View()
	row, ok := view.GetOfferRow(offer.GetId())
	if !ok {
		return reviewPlace{}, false
	}

	anchor, ok := view.GetAnchor(row)
	if !ok || anchor.FilePath == "" {
		return reviewPlace{}, false
	}

	place := reviewPlace{anchor: *anchor, base: view.GetBaseSHA(), snapshot: view.GetSnapshotSHA()}
	if place.snapshot == "" {
		place.snapshot = r.round.GetSnapshotSha()
	}

	return place, true
}

func acceptTitle(offer *pullrequestv1.ForgeThread) string {
	logins := slices.Compact(slices.Sorted(slices.Values(xslice.Map(offer.GetComments(), describeCommentAuthor))))
	if len(logins) != 1 {
		return "Accept the forge thread"
	}
	if len(offer.GetComments()) == 1 {
		return "Accept " + logins[0] + "'s comment"
	}

	return "Accept " + logins[0] + "'s comments"
}

// acceptCmd sends the forge thread as the popup draws it now, with the note written under it.
func (r reviewScreen) acceptCmd(m *model, accept reviewAccept, note string) tea.Cmd {
	offer, ok := r.findOffer(m, accept.offerID)
	if !ok {
		m.note(messageWarning, "that comment is gone from the forge")

		return nil
	}

	agents, agentID, anchor := m.clients().agents, r.agentID, accept.place.anchor
	seen := xslice.Map(offer.GetComments(), func(comment *pullrequestv1.ForgeComment) *agentv1.SeenForgeComment {
		return &agentv1.SeenForgeComment{Id: comment.GetId(), EditedAt: comment.EditedAt}
	})

	return r.writeCmd(m, m.beginWrite("accepting the forge comment"), "accepted into your review", func(ctx context.Context) error {
		_, err := agents.AcceptForgeThread(ctx, connect.NewRequest(&agentv1.AcceptForgeThreadRequest{
			AgentId:       agentID,
			ForgeThreadId: accept.offerID,
			Seen:          seen,
			BaseSha:       accept.place.base,
			SnapshotSha:   accept.place.snapshot,
			FilePath:      anchor.FilePath,
			OldStart:      int32(anchor.OldStart),
			OldEnd:        int32(anchor.OldEnd),
			NewStart:      int32(anchor.NewStart),
			NewEnd:        int32(anchor.NewEnd),
			QuotedLines:   anchor.QuotedLines,
			Note:          note,
		}))

		return err
	})
}

// acceptingView is the forge thread being accepted above the composer.
func (r reviewScreen) acceptingView(m *model, width int) string {
	if r.threads.accepting == nil {
		return ""
	}

	offer, ok := r.findOffer(m, r.threads.accepting.offerID)
	if !ok {
		return ""
	}

	return r.offerView(m, offer, width)
}

func anchorOfThread(thread *agentv1.ReviewThread) diffview.Anchor {
	return diffview.Anchor{
		FilePath:    thread.GetFilePath(),
		OldStart:    int(thread.GetOldStart()),
		OldEnd:      int(thread.GetOldEnd()),
		NewStart:    int(thread.GetNewStart()),
		NewEnd:      int(thread.GetNewEnd()),
		QuotedLines: thread.GetQuotedLines(),
	}
}
