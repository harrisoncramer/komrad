package review

import (
	"fmt"

	agentv1 "weave/proto/gen/agent/v1"
)

// anchorKey is the file and line a thread is pinned to, with the side included since a thread
// on a deleted line has no new-side number.
type anchorKey struct {
	filePath string
	newLine  int32
	oldLine  int32
}

func keyForThread(thread *agentv1.ReviewThread) anchorKey {
	return anchorKey{
		filePath: thread.GetFilePath(),
		newLine:  thread.GetNewStart(),
		oldLine:  thread.GetOldStart(),
	}
}

// placement is how a thread reached the row it is drawn on, which is the difference between a
// thread the diff still agrees with and one the agent has written out from under.
type placement uint8

const (
	// pinned is a thread whose line still reads the way it did when it was written.
	pinned placement = iota

	// ghosted is a thread placed by its old-side line onto code that has changed since, so the
	// place is right and the content is not.
	ghosted

	// adrift is a thread with nothing left to stand on, which keeps a row of its own at the
	// foot of the diff.
	adrift

	// collapsed is a thread in a file that has been read and folded away, which has its line back
	// the moment that file is opened again and so is neither adrift nor given a row at the foot.
	collapsed
)

// lineIndex is the diff's lines looked at from the three directions a thread can be placed
// from: the coordinates it was written against, its line on the old side, and its text.
type lineIndex struct {
	byKey       map[anchorKey]int
	byOld       map[oldKey]int
	byQuote     map[quoteKey][]int
	byBareQuote map[quoteKey][]int
}

type oldKey struct {
	filePath string
	oldLine  int
}

type quoteKey struct {
	filePath string
	quote    string
}

func (v *View) indexLines() lineIndex {
	index := lineIndex{
		byKey:       map[anchorKey]int{},
		byOld:       map[oldKey]int{},
		byQuote:     map[quoteKey][]int{},
		byBareQuote: map[quoteKey][]int{},
	}

	for y, current := range v.rows {
		quote, ok := v.quoteAt(y)
		if !ok {
			continue
		}

		file := v.patch.Files[current.file].Path()
		line := v.patch.Files[current.file].Hunks[current.hunk].Lines[current.line]

		index.byKey[anchorKey{
			filePath: file,
			newLine:  int32(line.NewNumber),
			oldLine:  int32(line.OldNumber),
		}] = y

		// A row answers to the spelling the diff draws it as.
		key := quoteKey{filePath: file, quote: quote}
		index.byQuote[key] = append(index.byQuote[key], y)

		// An agent stores a quote as bare code.
		if stripped := strippedQuote(quote); stripped != quote {
			bare := quoteKey{filePath: file, quote: stripped}
			index.byBareQuote[bare] = append(index.byBareQuote[bare], y)
		}

		if line.OldNumber > 0 {
			index.byOld[oldKey{filePath: file, oldLine: line.OldNumber}] = y
		}
	}

	return index
}

// rowFor is the row a thread is drawn on and what that placing is worth, trying the
// coordinates it was written against, then its old-side line, then its text.
func (v *View) rowFor(index lineIndex, thread *agentv1.ReviewThread) (int, placement) {
	quote := ""
	if lines := thread.GetQuotedLines(); len(lines) > 0 {
		quote = lines[0]
	}

	if y, ok := index.byKey[keyForThread(thread)]; ok {
		return y, v.placementFor(y, quote)
	}

	// The base commit holds the old-side line still however much the agent rewrites the new one,
	// which stops being true the moment the old side comes from somewhere else.
	if old := int(thread.GetOldStart()); old > 0 && !v.oldSideForeign {
		if y, ok := index.byOld[oldKey{filePath: thread.GetFilePath(), oldLine: old}]; ok {
			return y, v.placementFor(y, quote)
		}
	}

	// Text catches a line that moved rather than changed, and two matches place nothing rather
	// than guess between them.
	if found := index.quoted(thread.GetFilePath(), quote); quote != "" && len(found) == 1 {
		return found[0], pinned
	}

	return 0, adrift
}

// doesQuoteMatch reports whether the line drawn at a row is the line a thread quoted, comparing
// without the diff marker only for a quote stored without one.
func doesQuoteMatch(drawn, stored string) bool {
	if stored == "" || doesCarryMarker(stored) {
		return drawn == stored
	}

	return strippedQuote(drawn) == stored
}

// doesCarryMarker reports whether a quoted line was stored with the marker the diff draws in
// front of it.
func doesCarryMarker(quote string) bool {
	if quote == "" {
		return false
	}

	switch quote[0] {
	case '+', '-', ' ':
		return true
	}

	return false
}

// strippedQuote is a quoted line with its diff marker taken off the front.
func strippedQuote(quote string) string {
	if quote == "" {
		return quote
	}

	switch quote[0] {
	case '+', '-', ' ':
		return quote[1:]
	}

	return quote
}

// quoted is the rows a thread's quote names, read only against the spelling it was stored in.
func (index lineIndex) quoted(filePath, quote string) []int {
	// Explainer: a drawn line always carries a marker and a bare quote never does, so the two
	// spellings can never name the same row and the maps must not be crossed.
	key := quoteKey{filePath: filePath, quote: quote}
	if doesCarryMarker(quote) {
		return index.byQuote[key]
	}

	return index.byBareQuote[key]
}

// placementFor is whether the line a thread landed on still reads the way the thread was
// written against it.
func (v *View) placementFor(y int, quote string) placement {
	if text, _ := v.quoteAt(y); doesQuoteMatch(text, quote) {
		return pinned
	}

	return ghosted
}

// FormatLocation names a place in the diff, on whichever side the line actually lives.
func FormatLocation(filePath string, oldStart, newStart int) string {
	if newStart > 0 {
		return fmt.Sprintf("%s:%d", filePath, newStart)
	}

	return fmt.Sprintf("%s:%d (removed)", filePath, oldStart)
}

// FormatThreadLocation formats a thread's location for callers outside a new anchor.
func FormatThreadLocation(thread *agentv1.ReviewThread) string {
	return FormatLocation(thread.GetFilePath(), int(thread.GetOldStart()), int(thread.GetNewStart()))
}

// lineOf is the line a thread was written against, on whichever side it lives.
func lineOf(thread *agentv1.ReviewThread) int32 {
	if thread.GetNewStart() > 0 {
		return thread.GetNewStart()
	}

	return thread.GetOldStart()
}
