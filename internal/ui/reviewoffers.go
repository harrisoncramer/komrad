package ui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"komrad/internal/assemble"
	"komrad/internal/ui/diffview"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
)

// listOffers is the forge's threads on the agent's open pull request, the ones on a line first in
// the order a reader meets them, then the ones on the change as a whole.
func (r reviewScreen) listOffers(s panelShared) []*pullrequestv1.ForgeThread {
	offers := slices.Clone(assemble.Offers(s.listing.world, r.agentID))
	slices.SortStableFunc(offers, func(a, b *pullrequestv1.ForgeThread) int {
		return cmp.Or(
			compareFirst(a.GetPath() != "", b.GetPath() != ""),
			compareFirst(!a.GetResolved(), !b.GetResolved()),
			strings.Compare(a.GetPath(), b.GetPath()),
			cmp.Compare(a.GetLine(), b.GetLine()),
		)
	})

	return offers
}

// doesStepOffers reports whether J walks the forge's comments rather than jumping to where a
// comment of the reader's was written.
func (r reviewScreen) doesStepOffers(s panelShared) bool {
	if r.doesOfferJump() {
		return false
	}

	return slices.ContainsFunc(r.listOffers(s), isPlacedOffer)
}

func isPlacedOffer(offer *pullrequestv1.ForgeThread) bool {
	return offer.GetPath() != "" && !offer.GetDismissed()
}

// stepOffer moves the cursor onto the next forge comment written on a line, and chooses it.
func (r *reviewScreen) stepOffer(s panelShared) tea.Cmd {
	view := r.diff.View()
	placed := slices.DeleteFunc(r.listOffers(s), func(offer *pullrequestv1.ForgeThread) bool { return !isPlacedOffer(offer) })
	if len(placed) == 0 || view == nil {
		return nil
	}

	slices.SortStableFunc(placed, func(a, b *pullrequestv1.ForgeThread) int {
		rowA, okA := view.GetOfferRow(a.GetId())
		rowB, okB := view.GetOfferRow(b.GetId())

		return cmp.Or(compareFirst(okA, okB), cmp.Compare(rowA, rowB))
	})

	next := 0
	if at := slices.IndexFunc(placed, func(offer *pullrequestv1.ForgeThread) bool { return offer.GetId() == r.offer }); at >= 0 {
		next = (at + 1) % len(placed)
	}
	r.offer = placed[next].GetId()

	row, ok := view.GetOfferRow(r.offer)
	if !ok {
		r.notice = fmt.Sprintf("%s is not in this diff", describeOfferPlace(placed[next]))

		return nil
	}

	return r.moveCursor(s, row)
}

// offerView is a forge thread's comments wrapped to the width given, oldest first, with where it
// lives on the forge under them.
func (r reviewScreen) offerView(offer *pullrequestv1.ForgeThread, width int) string {
	var out strings.Builder
	for _, comment := range offer.GetComments() {
		head := drawCommentHead(diffview.OfferTone, describeCommentAuthor(comment)+" on the forge", comment.GetWrittenAt())
		if comment.GetEditedAt() != "" {
			head += " " + dimStyle().Render("(edited)")
		}

		out.WriteString(head + "\n" + reviewWrap(width, strings.TrimSpace(plainOfferText(comment.GetBody()))) + "\n\n")
	}

	foot := plainOfferText(offer.GetUrl())
	if offer.GetResolved() {
		foot = strings.TrimSpace("resolved  " + foot)
	}
	if offer.GetDismissed() {
		foot = strings.TrimSpace("dismissed  " + foot)
	}
	if foot != "" {
		out.WriteString(dimStyle().Render(ansi.Hardwrap(foot, max(width, 1), false)) + "\n")
	}

	return out.String()
}

func describeCommentAuthor(comment *pullrequestv1.ForgeComment) string {
	switch {
	case comment.GetYours():
		return "you"
	case comment.GetBot() && comment.GetAuthorLogin() != "":
		return describeForgeLogin(comment.GetAuthorLogin()) + " (bot)"
	}

	return describeForgeLogin(comment.GetAuthorLogin())
}

func describeForgeLogin(login string) string {
	if login == "" {
		return "someone"
	}

	return "@" + login
}

func describeOfferPlace(offer *pullrequestv1.ForgeThread) string {
	if offer.GetPath() == "" {
		return "change as a whole"
	}
	if offer.GetLine() == 0 {
		return offer.GetPath()
	}
	if offer.GetOldSide() {
		return diffview.FormatLocation(offer.GetPath(), int(offer.GetLine()), 0)
	}

	return diffview.FormatLocation(offer.GetPath(), 0, int(offer.GetLine()))
}

// plainOfferText is text a stranger wrote with its escape sequences and control characters taken
// out, so it can never restyle the panel or reach the terminal.
func plainOfferText(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < ' ', r == 0x7f, r >= 0x80 && r <= 0x9f:
			return -1
		default:
			return r
		}
	}, ansi.Strip(text))
}

// compareFirst orders true before false.
func compareFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}

	return 1
}

// offerAtCursor is the forge thread the cursor stands on: the one J last chose when it is there,
// or else the first on the row.
func (r reviewScreen) offerAtCursor(s panelShared) (*pullrequestv1.ForgeThread, bool) {
	view := r.diff.View()
	if view == nil {
		return nil, false
	}

	here := view.GetOffersAt(r.diff.Cursor())
	if len(here) == 0 {
		return nil, false
	}

	id := here[0].ID
	if slices.ContainsFunc(here, func(offer diffview.Offer) bool { return offer.ID == r.offer }) {
		id = r.offer
	}

	offers := r.listOffers(s)
	at := slices.IndexFunc(offers, func(offer *pullrequestv1.ForgeThread) bool { return offer.GetId() == id })
	if at < 0 {
		return nil, false
	}

	return offers[at], true
}

// reviewOffersTracedMsg is where git says the forge's comments went, for the comments and the
// snapshot key names.
type reviewOffersTracedMsg struct {
	agentID string
	key     string
	traced  map[string]diffview.Traced
}

// reviewOffers is the forge's comments as they were last pinned to the diff on screen.
type reviewOffers struct {
	key      string
	snapshot string
	view     *diffview.View
	anchors  []diffview.Offer
	traced   map[string]diffview.Traced
}

// followOffers pins the forge's comments to a diff drawn afresh, and asks git where their lines
// went whenever the comments or the snapshot under them change.
func (r *reviewScreen) followOffers(s panelShared) tea.Cmd {
	view := r.diff.View()
	if view == nil {
		return nil
	}

	offers := r.listOffers(s)
	key := keyOffers(view.GetSnapshotSHA(), offers)
	if key == r.offers.key && view == r.offers.view {
		return nil
	}

	if r.offers.snapshot != view.GetSnapshotSHA() {
		r.offers.traced = nil
	}

	fresh := key != r.offers.key
	r.offers.key, r.offers.snapshot, r.offers.view = key, view.GetSnapshotSHA(), view
	r.offers.anchors = xslice.Map(offers, anchorOffer)
	r.pinOffers()

	if !fresh || s.git == nil {
		return nil
	}

	return traceOffersCmd(r.agentID, key, traceOffersParams{
		Git:    s.git,
		Dir:    r.dir,
		ToSHA:  view.GetSnapshotSHA(),
		Offers: offers,
	})
}

func (r *reviewScreen) applyOffersTraced(msg reviewOffersTracedMsg) {
	if msg.agentID != r.agentID || msg.key != r.offers.key || r.diff.View() == nil {
		return
	}

	r.offers.traced = msg.traced
	r.pinOffers()
}

func (r *reviewScreen) pinOffers() {
	r.offers.view.SetOfferTraced(r.offers.traced)
	r.offers.view.PinOffers(r.offers.anchors)
}

func traceOffersCmd(agentID, key string, params traceOffersParams) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), traceTimeout)
		defer cancel()

		return reviewOffersTracedMsg{
			agentID: agentID,
			key:     key,
			traced:  traceOffers(ctx, params),
		}
	}
}

// keyOffers names the forge's comments by the snapshot and everything that places them.
func keyOffers(snapshot string, offers []*pullrequestv1.ForgeThread) string {
	var key strings.Builder
	key.WriteString(snapshot)
	for _, offer := range offers {
		fmt.Fprintf(&key, "\x00%s\x00%s\x00%d\x00%t\x00%s\x00%s\x00%t\x00%t",
			offer.GetId(), offer.GetPath(), offer.GetLine(), offer.GetOldSide(), offer.GetCommitSha(), offer.GetQuote(), offer.GetResolved(), offer.GetDismissed())
	}

	return key.String()
}

func anchorOffer(offer *pullrequestv1.ForgeThread) diffview.Offer {
	return diffview.Offer{
		ID:        offer.GetId(),
		FilePath:  offer.GetPath(),
		Line:      offer.GetLine(),
		OldSide:   offer.GetOldSide(),
		Quote:     offer.GetQuote(),
		Resolved:  offer.GetResolved(),
		Dismissed: offer.GetDismissed(),
	}
}

// doesDismissOffer reports whether the cursor stands on a forge comment, with no thread of the
// reader's in front of it.
func (r reviewScreen) doesDismissOffer(s panelShared) bool {
	if _, ok := r.diff.threadAtCursor(); ok {
		return false
	}

	_, ok := r.offerAtCursor(s)

	return ok
}

// dismissOfferDesc names what the dismiss key does to the forge comment under the cursor.
func (r reviewScreen) dismissOfferDesc(s panelShared) string {
	if offer, ok := r.offerAtCursor(s); ok && offer.GetDismissed() {
		return "restore the forge comment"
	}

	return "dismiss the forge comment"
}

// dismissOfferCmd marks the forge comment under the cursor dismissed, or active again when it was.
func (r reviewScreen) dismissOfferCmd(s panelShared) tea.Cmd {
	offer, ok := r.offerAtCursor(s)
	if !ok {
		return nil
	}

	pullRequests, id, restore := s.api.pullRequests, offer.GetId(), offer.GetDismissed()
	said := "dismissed the comment from the forge"
	if restore {
		said = "restored the comment from the forge"
	}

	return r.writeCmd(s, said, func(ctx context.Context) error {
		_, err := pullRequests.DismissForgeThread(ctx, connect.NewRequest(&pullrequestv1.DismissForgeThreadRequest{Id: id, Restore: restore}))

		return err
	})
}
