package review

import "strings"

// Suggestion is the replacement a reviewer proposes for a run of lines, held in the round's own
// line numbers since that is what the comment carrying it is pinned to.
type Suggestion struct {
	FilePath string
	NewStart int
	NewEnd   int
	Lines    []string
}

// SplitSource cuts a file into the lines a suggestion is measured in, dropping the newline the
// last line ends on so that both sides of the comparison are cut the same way.
func SplitSource(source string) []string {
	return strings.Split(strings.TrimSuffix(source, "\n"), "\n")
}

// Suggest is the one replacement covering everything between the first and last line the reviewer
// changed, which is a range a comment can be pinned to however many separate edits they made.
func Suggest(filePath string, round, edited []string) (Suggestion, bool) {
	if len(round) == 0 {
		return Suggestion{}, false
	}

	prefix := 0
	for prefix < len(round) && prefix < len(edited) && round[prefix] == edited[prefix] {
		prefix++
	}

	if prefix == len(round) && prefix == len(edited) {
		return Suggestion{}, false
	}

	suffix := 0
	for suffix < len(round)-prefix && suffix < len(edited)-prefix &&
		round[len(round)-1-suffix] == edited[len(edited)-1-suffix] {
		suffix++
	}

	suggestion := Suggestion{
		FilePath: filePath,
		NewStart: prefix + 1,
		NewEnd:   len(round) - suffix,
		Lines:    edited[prefix : len(edited)-suffix],
	}

	if suggestion.NewEnd < suggestion.NewStart {
		return widen(suggestion, round), true
	}

	return suggestion, true
}

// widen gives an insertion the neighbouring line to replace, since a range holding nothing is a
// place in the file rather than something a comment can stand on.
func widen(suggestion Suggestion, round []string) Suggestion {
	if suggestion.NewStart > 1 {
		suggestion.NewStart--
		suggestion.NewEnd = suggestion.NewStart
		suggestion.Lines = append([]string{round[suggestion.NewStart-1]}, suggestion.Lines...)

		return suggestion
	}

	suggestion.NewStart, suggestion.NewEnd = 1, 1
	suggestion.Lines = append(append([]string{}, suggestion.Lines...), round[0])

	return suggestion
}
