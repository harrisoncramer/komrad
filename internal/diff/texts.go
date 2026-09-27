package diff

import (
	"fmt"
	"strings"

	"komrad/lib/xslice"

	"github.com/pmezard/go-difflib/difflib"
)

// textContext is how many unchanged lines are kept either side of a change.
const textContext = 3

// CompareTexts is the change between two texts as one file under a path, with both sides
// carried whole.
func CompareTexts(path, before, after string) File {
	oldLines, newLines := splitText(before), splitText(after)

	return File{
		OldPath:   path,
		NewPath:   path,
		OldSource: before,
		NewSource: after,
		Hunks: xslice.Map(difflib.NewMatcher(oldLines, newLines).GetGroupedOpCodes(textContext), func(group []difflib.OpCode) Hunk {
			return buildHunk(group, oldLines, newLines)
		}),
	}
}

func buildHunk(group []difflib.OpCode, oldLines, newLines []string) Hunk {
	first, last := group[0], group[len(group)-1]
	hunk := Hunk{
		Header: fmt.Sprintf("@@ -%s +%s @@",
			formatRange(first.I1, last.I2), formatRange(first.J1, last.J2)),
		NewStart: first.J1 + 1,
	}

	for _, op := range group {
		if op.Tag == 'e' {
			for offset := range op.I2 - op.I1 {
				hunk.Lines = append(hunk.Lines, Line{
					Kind:      Context,
					Text:      oldLines[op.I1+offset],
					OldNumber: op.I1 + offset + 1,
					NewNumber: op.J1 + offset + 1,
				})
			}

			continue
		}

		for i := op.I1; i < op.I2; i++ {
			hunk.Lines = append(hunk.Lines, Line{
				Kind:      Deletion,
				Text:      oldLines[i],
				OldNumber: i + 1,
			})
		}
		for j := op.J1; j < op.J2; j++ {
			hunk.Lines = append(hunk.Lines, Line{
				Kind:      Addition,
				Text:      newLines[j],
				NewNumber: j + 1,
			})
		}
	}

	return hunk
}

// formatRange is one side of a hunk header as git writes it.
func formatRange(start, end int) string {
	switch length := end - start; length {
	case 0:
		return fmt.Sprintf("%d,0", start)
	case 1:
		return fmt.Sprint(start + 1)
	default:
		return fmt.Sprintf("%d,%d", start+1, length)
	}
}

func splitText(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}

	return strings.Split(text, "\n")
}
