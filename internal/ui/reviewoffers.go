package ui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"komrad/internal/assemble"
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// reviewOfferTone is what the forge's comments are drawn in, so they never pass for the reader's.
const reviewOfferTone = paint.ToneWarning

// offerBodyRows is how many rows the chosen offer's comment gets.
const offerBodyRows = 4

// offersThreadRows is how many rows the conversation below the forge's comments keeps.
const offersThreadRows = 3

// offerRows is the most rows the forge's comments take, the chosen one's read-out included.
const offerRows = 9

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
	return offer.GetPath() != ""
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

// offersView is the forge's comments on the pull request, the chosen one read out in full.
func (r reviewScreen) offersView(s panelShared, room int) string {
	if _, history := r.isHistory(); room <= 0 || history {
		return ""
	}

	offers := r.listOffers(s)
	if len(offers) == 0 {
		return ""
	}

	open := 0
	for _, offer := range offers {
		if !offer.GetResolved() {
			open++
		}
	}

	out := paint.Panel.Style(reviewOfferTone).Bold(true).Render("From the forge") +
		dimStyle.Render(fmt.Sprintf("  %d open, %d resolved", open, len(offers)-open)) + "\n"

	rows := make([][]string, len(offers))
	chosen := r.chosenOffer()
	chosenAt := -1
	for i, offer := range offers {
		rows[i] = r.offerRow(s, offer, offer.GetId() == chosen, room-1)
		if offer.GetId() == chosen {
			chosenAt = i
		}
	}

	start := 0
	shown := fitOfferRows(rows, room)
	if chosenAt >= shown {
		start = chosenAt
		shown = fitOfferRows(rows[start:], room)
	}
	for _, row := range rows[start : start+shown] {
		out += strings.Join(row, "\n") + "\n"
	}

	if left := len(offers) - shown; left > 0 {
		out += dimStyle.Render(fmt.Sprintf(" + %d more", left)) + "\n"
	}

	return out
}

// fitOfferRows is how many offers from the first fit in room, keeping a row for the count of the
// rest when they do not all fit.
func fitOfferRows(rows [][]string, room int) int {
	used := 0
	for i, row := range rows {
		need := len(row)
		if i < len(rows)-1 {
			need++
		}
		if used+need > room {
			return i
		}
		used += len(row)
	}

	return len(rows)
}

// offerRow is one forge thread, who started it and where, with at most extra rows of its comment
// under it when it is the one chosen.
func (r reviewScreen) offerRow(s panelShared, offer *pullrequestv1.ForgeThread, chosen bool, extra int) []string {
	style := dimStyle
	if chosen {
		style = paint.Panel.Style(reviewOfferTone).Bold(true)
	}

	author := describeOfferAuthor(offer)
	room := max(lineWidthOf(s.pane)-len(author)-6, 1)
	where := clipToTail(describeOfferPlace(offer), room)
	if offer.GetResolved() {
		where += " (resolved)"
	}

	head := paint.Panel.Style(reviewOfferTone).Render(" "+glyph.Right+" ") +
		style.Render(author) + dimStyle.Render("  "+where)
	comments := offer.GetComments()
	if !chosen || len(comments) == 0 || extra <= 0 {
		return []string{head}
	}

	body := paint.WrapRows(strings.TrimSpace(plainOfferText(comments[0].GetBody())), max(lineWidthOf(s.pane)-4, 1))
	if len(body) > offerBodyRows {
		body = append(body[:offerBodyRows-1], "…")
	}
	under := xslice.Map(body, func(row string) string { return "    " + row })
	if replies := len(comments) - 1; replies > 0 {
		under = append(under, dimStyle.Render(fmt.Sprintf("    + %d repl%s", replies, pluralSuffix(replies, "y", "ies"))))
	}
	for _, row := range strings.Split(ansi.Hardwrap(plainOfferText(offer.GetUrl()), max(lineWidthOf(s.pane)-4, 1), false), "\n") {
		if row != "" {
			under = append(under, dimStyle.Render("    "+row))
		}
	}

	return append([]string{head}, under[:min(len(under), extra)]...)
}

func describeOfferAuthor(offer *pullrequestv1.ForgeThread) string {
	comments := offer.GetComments()
	if len(comments) == 0 {
		return "someone"
	}

	first := comments[0]
	switch {
	case first.GetYours():
		return "you"
	case first.GetAuthorLogin() == "":
		return "someone"
	case first.GetBot():
		return "@" + first.GetAuthorLogin() + " (bot)"
	}

	return "@" + first.GetAuthorLogin()
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

	return fmt.Sprintf("%s:%d", offer.GetPath(), offer.GetLine())
}

func pluralSuffix(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
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

// countOffersRoom counts the rows the forge's comments get once the sidebar's chrome and the
// reader's own comments have taken theirs.
func countOffersRoom(spare, taken int) int {
	return min(max(spare-2-offersThreadRows-taken, 0), offerRows)
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

// clipToTail cuts a path from its front so its line number survives.
func clipToTail(said string, room int) string {
	if lipgloss.Width(said) <= room {
		return said
	}

	return ansi.TruncateLeft(said, lipgloss.Width(said)-room+1, "…")
}

// chosenOffer is the forge comment read out in full: one the cursor is standing on, or else the
// one J last chose.
func (r reviewScreen) chosenOffer() string {
	view := r.diff.View()
	if view == nil {
		return r.offer
	}

	here := view.GetOffersAt(r.diff.Cursor())
	if len(here) == 0 || slices.ContainsFunc(here, func(offer diffview.Offer) bool { return offer.ID == r.offer }) {
		return r.offer
	}

	return here[0].ID
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
		fmt.Fprintf(&key, "\x00%s\x00%s\x00%d\x00%t\x00%s\x00%s\x00%t",
			offer.GetId(), offer.GetPath(), offer.GetLine(), offer.GetOldSide(), offer.GetCommitSha(), offer.GetQuote(), offer.GetResolved())
	}

	return key.String()
}

func anchorOffer(offer *pullrequestv1.ForgeThread) diffview.Offer {
	return diffview.Offer{
		ID:       offer.GetId(),
		FilePath: offer.GetPath(),
		Line:     offer.GetLine(),
		OldSide:  offer.GetOldSide(),
		Quote:    offer.GetQuote(),
		Resolved: offer.GetResolved(),
	}
}
