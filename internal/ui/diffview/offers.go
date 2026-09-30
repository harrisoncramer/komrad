package diffview

import (
	"slices"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"charm.land/lipgloss/v2"
)

// OfferTone is the tone the forge's comments are drawn in.
const OfferTone = paint.ToneUserGithub

// Offer is a comment the forge holds on a file, placed on the diff the way a thread is and kept
// apart from the reader's own.
type Offer struct {
	ID        string
	FilePath  string
	Line      int32
	OldSide   bool
	Quote     string
	Resolved  bool
	Dismissed bool
}

// SetOfferTraced holds where git says each offer's line went, keyed by offer id, and is read by
// the next pass that pins the offers.
func (v *View) SetOfferTraced(traced map[string]Traced) {
	v.offerTraced = traced
}

// PinOffers works out the row each offer stands on: where git says its line went, then the one
// line reading the way its quote does, then the header of its file.
func (v *View) PinOffers(offers []Offer) {
	v.offers = offers
	v.offered = map[int][]Offer{}
	v.offerRows = map[string]int{}

	index := v.indexLines()
	for _, offer := range offers {
		if offer.FilePath == "" {
			continue
		}

		y, ok := v.offerRowOf(index, offer)
		if !ok {
			y, ok = v.fileHeader(v.offerPathOf(offer))
		}
		if ok {
			v.offered[y] = append(v.offered[y], offer)
			v.offerRows[offer.ID] = y
		}
	}
}

func (v *View) offerRowOf(index lineIndex, offer Offer) (int, bool) {
	if offer.Line <= 0 || v.viewed[v.offerPathOf(offer)] {
		return 0, false
	}

	if y, ok := v.offerRowByTrace(index, offer); ok {
		return y, true
	}

	return v.offerRowByQuote(index, offer)
}

// offerPathOf is the file an offer is about in the snapshot on screen, which a rename moves.
func (v *View) offerPathOf(offer Offer) string {
	if found, ok := v.offerTraced[offer.ID]; ok && found.FilePath != "" {
		return found.FilePath
	}

	return offer.FilePath
}

func (v *View) offerRowByTrace(index lineIndex, offer Offer) (int, bool) {
	found, ok := v.offerTraced[offer.ID]
	if !ok || offer.OldSide || found.FileOnly || found.NewStart <= 0 {
		return 0, false
	}

	y, ok := index.byNew[newKey{filePath: found.FilePath, newLine: found.NewStart}]

	return y, ok
}

// offerRowByQuote is the one row on the offer's side drawing the line it was written on.
func (v *View) offerRowByQuote(index lineIndex, offer Offer) (int, bool) {
	if offer.Quote == "" {
		return 0, false
	}

	_, bare := index.quoted(v.offerPathOf(offer), offer.Quote)
	found := slices.DeleteFunc(slices.Clone(bare), func(y int) bool {
		line, ok := v.getLine(y)
		if !ok {
			return true
		}
		if offer.OldSide {
			return line.OldNumber <= 0
		}

		return line.NewNumber <= 0
	})
	if len(found) != 1 {
		return 0, false
	}

	return found[0], true
}

// GetOffersAt is the forge's comments drawn on a row.
func (v *View) GetOffersAt(y int) []Offer {
	return v.offered[y]
}

// GetOfferRow is the row an offer stands on, false for one the diff has nowhere for.
func (v *View) GetOfferRow(id string) (int, bool) {
	y, ok := v.offerRows[id]

	return y, ok
}

// offerMarkFor is the glyph the forge's comments on a row get in the gutter, settled once every
// one of them is resolved.
func (v *View) offerMarkFor(offers []Offer) (string, lipgloss.Style) {
	if slices.ContainsFunc(offers, func(offer Offer) bool { return !offer.Resolved && !offer.Dismissed }) {
		return v.glyphs.Spell(glyph.ThreadRead), paint.Panel.Style(OfferTone)
	}

	if slices.ContainsFunc(offers, func(offer Offer) bool { return offer.Dismissed }) {
		return v.glyphs.Spell(glyph.ThreadDismissed), paint.Panel.Style(OfferTone)
	}

	return v.glyphs.Spell(glyph.ThreadSettled), paint.Panel.Style(OfferTone)
}
