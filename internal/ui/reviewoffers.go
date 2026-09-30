package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"komrad/internal/assemble"
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// reviewOfferTone is what the forge's comments are drawn in, so they never pass for the reader's.
const reviewOfferTone = paint.ToneWarning

// offerBodyRows is how many rows the chosen offer's comment gets.
const offerBodyRows = 4

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
	placed := slices.DeleteFunc(r.listOffers(s), func(offer *pullrequestv1.ForgeThread) bool { return !isPlacedOffer(offer) })
	if len(placed) == 0 || r.diff.View() == nil {
		return nil
	}

	next := 0
	if at := slices.IndexFunc(placed, func(offer *pullrequestv1.ForgeThread) bool { return offer.GetId() == r.offer }); at >= 0 {
		next = (at + 1) % len(placed)
	}
	r.offer = placed[next].GetId()

	row, ok := r.diff.View().Locate(diffview.Place{FilePath: placed[next].GetPath(), NewLine: int(placed[next].GetLine())})
	if !ok {
		r.notice = fmt.Sprintf("%s is not in this diff", describeOfferPlace(placed[next]))

		return nil
	}

	return r.moveCursor(s, row)
}

// offersView is the forge's comments on the pull request, the chosen one read out in full.
func (r reviewScreen) offersView(s panelShared, room int) string {
	if room <= 0 || !r.isYoursDrawn() {
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

	shown := min(room, len(offers))
	for _, offer := range offers[:shown] {
		out += r.offerRow(s, offer)
	}

	if left := len(offers) - shown; left > 0 {
		out += dimStyle.Render(fmt.Sprintf(" + %d more", left)) + "\n"
	}

	return out
}

// offerRow is one forge thread, who started it and where, with its comment under it when it is
// the one chosen.
func (r reviewScreen) offerRow(s panelShared, offer *pullrequestv1.ForgeThread) string {
	style := dimStyle
	chosen := offer.GetId() == r.offer
	if chosen {
		style = paint.Panel.Style(reviewOfferTone).Bold(true)
	}

	author := describeOfferAuthor(offer)
	room := max(lineWidthOf(s.pane)-len(author)-6, 1)
	where := clipToTail(describeOfferPlace(offer), room)
	if offer.GetResolved() {
		where += " (resolved)"
	}

	out := paint.Panel.Style(reviewOfferTone).Render(" "+glyph.Right+" ") +
		style.Render(author) + dimStyle.Render("  "+where) + "\n"
	if !chosen {
		return out
	}

	comments := offer.GetComments()
	if len(comments) == 0 {
		return out
	}

	body := paint.WrapRows(strings.TrimSpace(comments[0].GetBody()), max(lineWidthOf(s.pane)-4, 1))
	if len(body) > offerBodyRows {
		body = append(body[:offerBodyRows-1], "…")
	}
	for _, row := range body {
		out += "    " + row + "\n"
	}
	if replies := len(comments) - 1; replies > 0 {
		out += dimStyle.Render(fmt.Sprintf("    + %d repl%s", replies, pluralSuffix(replies, "y", "ies"))) + "\n"
	}
	for _, row := range strings.Split(ansi.Hardwrap(offer.GetUrl(), max(lineWidthOf(s.pane)-4, 1), false), "\n") {
		if row != "" {
			out += dimStyle.Render("    "+row) + "\n"
		}
	}

	return out
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

	return fmt.Sprintf("%s:%d", offer.GetPath(), offer.GetLine())
}

func pluralSuffix(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}
