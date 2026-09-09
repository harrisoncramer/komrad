package review

import (
	"slices"
	"strconv"
	"strings"

	"weave/internal/git"
)

// Suggestion is the replacement a reviewer proposes for a run of lines, held in the round's own
// line numbers.
type Suggestion struct {
	FilePath string
	NewStart int
	NewEnd   int
	Lines    []string
}

// Key identifies one proposal, so one already sent or turned down is not offered again.
func (s Suggestion) Key() string {
	return strings.Join(append([]string{
		s.FilePath,
		strconv.Itoa(s.NewStart),
		strconv.Itoa(s.NewEnd),
	}, s.Lines...), "\x00")
}

// SplitSource cuts a file into the lines a suggestion is measured in, dropping the newline the
// last line ends on.
func SplitSource(source string) []string {
	return strings.Split(strings.TrimSuffix(source, "\n"), "\n")
}

// Suggest is what one run of edited lines proposes, widened onto the neighbouring line when the
// run puts lines in without replacing any, and refused when it proposes what the round has.
func Suggest(edit git.Edit, round []string) (Suggestion, bool) {
	if len(round) == 0 || edit.Start < 1 && edit.End < 1 {
		return Suggestion{}, false
	}

	suggestion := Suggestion{
		FilePath: edit.Path,
		NewStart: edit.Start,
		NewEnd:   edit.End,
		Lines:    edit.Lines,
	}

	if suggestion.NewEnd < suggestion.NewStart {
		suggestion = widen(suggestion, round)
	}

	if suggestion.NewStart < 1 || suggestion.NewEnd > len(round) {
		return Suggestion{}, false
	}

	if slices.Equal(suggestion.Lines, round[suggestion.NewStart-1:suggestion.NewEnd]) {
		return Suggestion{}, false
	}

	return suggestion, true
}

// WithoutSuggestion is the file as it stands with one proposal put back the way the round holds
// it, rebuilt from the round's own lines so the runs left standing keep their numbering.
func WithoutSuggestion(round []string, edits []git.Edit, sent Suggestion) []string {
	kept := make([]git.Edit, 0, len(edits))

	for _, edit := range edits {
		if edit.Path != sent.FilePath || edit.Start < 1 || edit.End > len(round) || edit.End < edit.Start-1 {
			continue
		}

		if suggestion, ok := Suggest(edit, round); ok && suggestion.Key() == sent.Key() {
			continue
		}

		kept = append(kept, edit)
	}

	// Putting the later runs back first leaves the earlier ones standing where the round numbers
	// them, which is the numbering every run was measured in.
	slices.SortFunc(kept, func(a, b git.Edit) int { return b.Start - a.Start })

	out := slices.Clone(round)
	for _, edit := range kept {
		out = slices.Concat(out[:edit.Start-1], edit.Lines, out[edit.End:])
	}

	return out
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
	suggestion.Lines = append(slices.Clone(suggestion.Lines), round[0])

	return suggestion
}
