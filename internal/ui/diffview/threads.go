package diffview

import (
	"fmt"

	agentv1 "komrad/proto/gen/agent/v1"
)

// lineIndex looks at the diff's lines from the three directions that can place a thread: the line
// a traced position names, its line on the old side, and its text.
type lineIndex struct {
	byNew       map[newKey]int
	byOld       map[oldKey]int
	byQuote     map[quoteKey][]int
	byBareQuote map[quoteKey][]int
}

// anchorKey is one row's file and both sides' line numbers.
type anchorKey struct {
	filePath string
	newLine  int32
	oldLine  int32
}

// newKey is a line of the snapshot on screen.
type newKey struct {
	filePath string
	newLine  int32
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
		byNew:       map[newKey]int{},
		byOld:       map[oldKey]int{},
		byQuote:     map[quoteKey][]int{},
		byBareQuote: map[quoteKey][]int{},
	}

	for y, current := range v.rows {
		quote, ok := v.quoteAt(y)
		if !ok {
			continue
		}

		file := v.files[current.file].Path()
		line := v.files[current.file].Hunks[current.hunk].Lines[current.line]

		if line.NewNumber > 0 {
			index.byNew[newKey{filePath: file, newLine: int32(line.NewNumber)}] = y
		}

		// A row answers to the spelling the diff renders it as.
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

// quoteOf is the first line a thread quoted.
func quoteOf(thread *agentv1.ReviewThread) string {
	lines := thread.GetQuotedLines()
	if len(lines) == 0 {
		return ""
	}

	return lines[0]
}

// doesQuoteMatch reports whether a rendered line reads the way the thread stored its quote, under
// either spelling.
func doesQuoteMatch(rendered, stored string) bool {
	return rendered == stored || strippedQuote(rendered) == stored
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

// quoted is the rows answering a thread's quote, kept apart by the spelling each answered under.
func (i lineIndex) quoted(filePath, quote string) (rendered, bare []int) {
	key := quoteKey{filePath: filePath, quote: quote}

	return i.byQuote[key], i.byBareQuote[key]
}

// FormatLocation names a place in the diff, on whichever side the line actually lives.
func FormatLocation(filePath string, oldStart, newStart int) string {
	if newStart > 0 {
		return fmt.Sprintf("%s:%d", filePath, newStart)
	}

	return fmt.Sprintf("%s:%d (removed)", filePath, oldStart)
}

// FormatThreadLocation formats a thread's location for a caller holding no anchor.
func FormatThreadLocation(thread *agentv1.ReviewThread) string {
	if IsWholeChangeThread(thread) {
		return "the whole change"
	}

	return FormatLocation(thread.GetFilePath(), int(thread.GetOldStart()), int(thread.GetNewStart()))
}

// lineOf is the line a thread was written against, on whichever side it lives.
func lineOf(thread *agentv1.ReviewThread) int32 {
	if thread.GetNewStart() > 0 {
		return thread.GetNewStart()
	}

	return thread.GetOldStart()
}

// IsAgentThread reports whether an agent opened a conversation, read off the message that opened
// it.
func IsAgentThread(thread *agentv1.ReviewThread) bool {
	messages := thread.GetMessages()

	return len(messages) > 0 &&
		messages[0].GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT
}

// isUnsettled reports whether a conversation is one the person reviewing still owes an answer on.
func isUnsettled(thread *agentv1.ReviewThread) bool {
	return thread.GetResolvedAt() == "" && !IsAgentThread(thread)
}

// IsWholeChangeThread reports whether a thread is a remark on the change as a whole, with no line
// of its own.
func IsWholeChangeThread(thread *agentv1.ReviewThread) bool {
	return thread.GetFilePath() == ""
}
