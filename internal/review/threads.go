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
)

// lineIndex is the diff's lines looked at from the three directions a thread can be placed
// from: the coordinates it was written against, its line on the old side, and its text.
type lineIndex struct {
	byKey   map[anchorKey]int
	byOld   map[oldKey]int
	byQuote map[quoteKey][]int
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
		byKey:   map[anchorKey]int{},
		byOld:   map[oldKey]int{},
		byQuote: map[quoteKey][]int{},
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

		key := quoteKey{filePath: file, quote: quote}
		index.byQuote[key] = append(index.byQuote[key], y)

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

	// The base commit holds the old-side line still however much the agent rewrites the new one.
	if old := int(thread.GetOldStart()); old > 0 {
		if y, ok := index.byOld[oldKey{filePath: thread.GetFilePath(), oldLine: old}]; ok {
			return y, v.placementFor(y, quote)
		}
	}

	// Text catches a line that moved rather than changed, and two matches place nothing rather
	// than guess between them.
	if found := index.byQuote[quoteKey{filePath: thread.GetFilePath(), quote: quote}]; quote != "" && len(found) == 1 {
		return found[0], pinned
	}

	return 0, adrift
}

// placementFor is whether the line a thread landed on still reads the way the thread was
// written against it.
func (v *View) placementFor(y int, quote string) placement {
	if text, _ := v.quoteAt(y); text == quote {
		return pinned
	}

	return ghosted
}

// Where names a place in the diff, on whichever side the line actually lives.
func Where(filePath string, oldStart, newStart int) string {
	if newStart > 0 {
		return fmt.Sprintf("%s:%d", filePath, newStart)
	}

	return fmt.Sprintf("%s:%d (removed)", filePath, oldStart)
}

// WhereOf is Where for a thread, which is the form every caller outside a new anchor wants.
func WhereOf(thread *agentv1.ReviewThread) string {
	return Where(thread.GetFilePath(), int(thread.GetOldStart()), int(thread.GetNewStart()))
}

// lineOf is the line a thread was written against, on whichever side it lives.
func lineOf(thread *agentv1.ReviewThread) int32 {
	if thread.GetNewStart() > 0 {
		return thread.GetNewStart()
	}

	return thread.GetOldStart()
}
