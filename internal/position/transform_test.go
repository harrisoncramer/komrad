package position

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lines(start, end int) Range {
	return Range{Start: start, End: end}
}

// rewrite is one region of a file replaced by however many lines.
func rewrite(oldStart, oldEnd, newStart, newEnd int) RangeMapping {
	return RangeMapping{
		Old: lines(oldStart, oldEnd),
		New: lines(newStart, newEnd),
	}
}

// TestTransform walks the cases a caller can reach, refusals first.
func TestTransform(t *testing.T) {
	tests := []struct {
		name      string
		positions []Position
		mappings  []Mapping
		policy    Policy
		want      []Result
	}{
		{
			name:      "a file with no mapping at all is a file nothing happened to",
			positions: []Position{{Path: "a.go", Lines: lines(10, 12)}},
			mappings:  nil,
			policy:    OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(10, 12)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "a mapping for another file leaves this one alone",
			positions: []Position{{Path: "a.go", Lines: lines(10, 12)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "b.go", NewPath: "b.go"},
					Ranges: []RangeMapping{rewrite(1, 4, 1, 9)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(10, 12)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "a range starting before the first line of a file is nothing a file could hold",
			positions: []Position{{Path: "a.go", Lines: lines(0, 2)}},
			mappings: []Mapping{
				{File: FileMapping{OldPath: "a.go", NewPath: "a.go"}},
			},
			policy: FileOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(0, 2)},
					Outcome:  Dropped,
				},
			},
		},
		{
			name:      "a range ending before it starts is refused rather than shifted",
			positions: []Position{{Path: "a.go", Lines: lines(9, 4)}},
			mappings: []Mapping{
				{File: FileMapping{OldPath: "a.go", NewPath: "a.go"}},
			},
			policy: FileOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(9, 4)},
					Outcome:  Dropped,
				},
			},
		},
		{
			name:      "a deleted file is dropped however forgiving the policy",
			positions: []Position{{Path: "a.go", Lines: lines(10, 12)}},
			mappings: []Mapping{
				{File: FileMapping{OldPath: "a.go", NewPath: ""}},
			},
			policy: FileOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(10, 12)},
					Outcome:  Dropped,
				},
			},
		},
		{
			name:      "lines written over are dropped when the caller asked for nothing less",
			positions: []Position{{Path: "a.go", Lines: lines(10, 12)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(9, 13, 9, 11)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(10, 12)},
					Outcome:  Dropped,
				},
			},
		},
		{
			name:      "lines written over keep their file when the caller will take that",
			positions: []Position{{Path: "a.go", Lines: lines(10, 12)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "moved.go"},
					Ranges: []RangeMapping{rewrite(9, 13, 9, 11)},
				},
			},
			policy: FileOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "moved.go"},
					Outcome:  FileOnly,
				},
			},
		},
		{
			name:      "a change ending exactly where the range starts moved it and did not touch it",
			positions: []Position{{Path: "a.go", Lines: lines(10, 12)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(5, 10, 5, 12)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(12, 14)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "a change starting exactly where the range ends leaves it where it was",
			positions: []Position{{Path: "a.go", Lines: lines(10, 12)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(12, 20, 12, 14)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(10, 12)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "lines arriving above the range push it down",
			positions: []Position{{Path: "a.go", Lines: lines(10, 12)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(4, 4, 4, 7)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(13, 15)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "lines leaving above the range pull it up",
			positions: []Position{{Path: "a.go", Lines: lines(10, 12)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(2, 5, 2, 2)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(7, 9)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "lines arriving inside the range make it longer rather than moving it",
			positions: []Position{{Path: "a.go", Lines: lines(10, 20)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(14, 14, 14, 18)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(10, 24)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "a rename with edits above the range carries both",
			positions: []Position{{Path: "old/a.go", Lines: lines(30, 31)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "old/a.go", NewPath: "new/a.go"},
					Ranges: []RangeMapping{rewrite(1, 3, 1, 6)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "new/a.go", Lines: lines(33, 34)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "a file copied in two places answers for the position twice",
			positions: []Position{{Path: "a.go", Lines: lines(4, 5)}},
			mappings: []Mapping{
				{File: FileMapping{OldPath: "a.go", NewPath: "a.go"}},
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "copy.go"},
					Ranges: []RangeMapping{rewrite(1, 1, 1, 3)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(4, 5)},
					Outcome:  Placed,
				},
				{
					Index:    0,
					Position: Position{Path: "copy.go", Lines: lines(6, 7)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "mappings out of order are read in order anyway",
			positions: []Position{{Path: "a.go", Lines: lines(50, 52)}},
			mappings: []Mapping{
				{
					File: FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{
						rewrite(30, 32, 30, 34),
						rewrite(2, 4, 2, 3),
						rewrite(60, 61, 62, 70),
					},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(51, 53)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "a position on the file as a whole follows the rename and stays whole",
			positions: []Position{{Path: "old/a.go"}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "old/a.go", NewPath: "new/a.go"},
					Ranges: []RangeMapping{rewrite(1, 9, 1, 2)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "new/a.go"},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "an insertion point of its own is carried like any other range",
			positions: []Position{{Path: "a.go", Lines: lines(12, 12)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(1, 3, 1, 6)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(15, 15)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "an insertion point inside a rewritten region has nowhere to sit",
			positions: []Position{{Path: "a.go", Lines: lines(5, 5)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(3, 9, 3, 3)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(5, 5)},
					Outcome:  Dropped,
				},
			},
		},
		{
			name:      "an insertion point inside a replaced region keeps its file when asked",
			positions: []Position{{Path: "a.go", Lines: lines(5, 5)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(3, 9, 3, 5)},
				},
			},
			policy: FileOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go"},
					Outcome:  FileOnly,
				},
			},
		},
		{
			name:      "an insertion point where a region starts is the gap above it and stays",
			positions: []Position{{Path: "a.go", Lines: lines(3, 3)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(3, 9, 3, 5)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(3, 3)},
					Outcome:  Placed,
				},
			},
		},
		{
			name:      "an insertion point where a region ends moves with the lines below it",
			positions: []Position{{Path: "a.go", Lines: lines(9, 9)}},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(3, 9, 3, 5)},
				},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(5, 5)},
					Outcome:  Placed,
				},
			},
		},
		{
			name: "every position handed in is answered for",
			positions: []Position{
				{Path: "a.go", Lines: lines(10, 12)},
				{Path: "gone.go", Lines: lines(1, 2)},
				{Path: "b.go", Lines: lines(4, 5)},
			},
			mappings: []Mapping{
				{
					File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
					Ranges: []RangeMapping{rewrite(1, 1, 1, 2)},
				},
				{File: FileMapping{OldPath: "gone.go", NewPath: ""}},
			},
			policy: OmitOnConflict,
			want: []Result{
				{
					Index:    0,
					Position: Position{Path: "a.go", Lines: lines(11, 13)},
					Outcome:  Placed,
				},
				{
					Index:    1,
					Position: Position{Path: "gone.go", Lines: lines(1, 2)},
					Outcome:  Dropped,
				},
				{
					Index:    2,
					Position: Position{Path: "b.go", Lines: lines(4, 5)},
					Outcome:  Placed,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Transform(TransformParams{
				Positions: tt.positions,
				Mappings:  tt.mappings,
				Policy:    tt.policy,
			})

			assert.Equal(t, tt.want, got)
		})
	}
}

// script is an edit of a file as a list of regions replaced by however many lines.
type script []replacement

type replacement struct {
	oldStart int
	oldEnd   int
	newLen   int
}

// apply is the file the script leaves behind, with every line naming where it came from.
func (s script) apply(old []string) []string {
	next := make([]string, 0, len(old))
	cursor := 1
	for index, current := range s {
		for ; cursor < current.oldStart; cursor++ {
			next = append(next, old[cursor-1])
		}

		for written := range current.newLen {
			next = append(next, fmt.Sprintf("written %d.%d", index, written))
		}

		cursor = current.oldEnd
	}

	for ; cursor <= len(old); cursor++ {
		next = append(next, old[cursor-1])
	}

	return next
}

// mappings is the script said the way a diff would say it.
func (s script) mappings() []Mapping {
	ranges := make([]RangeMapping, 0, len(s))
	shift := 0
	for _, current := range s {
		newStart := current.oldStart + shift
		ranges = append(ranges, RangeMapping{
			Old: lines(current.oldStart, current.oldEnd),
			New: lines(newStart, newStart+current.newLen),
		})

		shift += current.newLen - (current.oldEnd - current.oldStart)
	}

	return []Mapping{
		{
			File:   FileMapping{OldPath: "a.go", NewPath: "a.go"},
			Ranges: ranges,
		},
	}
}

// doesTouch reports whether a line of the old file is one the script wrote over.
func (s script) doesTouch(line int) bool {
	return slices.ContainsFunc(s, func(current replacement) bool {
		return line >= current.oldStart && line < current.oldEnd
	})
}

func buildScript(random *rand.Rand, fileLines int) script {
	built := script{}
	cursor := 1
	for cursor < fileLines {
		gap := random.Intn(6)
		cursor += gap
		if cursor >= fileLines {
			break
		}

		taken := random.Intn(4)
		written := random.Intn(4)
		if taken == 0 && written == 0 {
			written = 1
		}

		built = append(built, replacement{
			oldStart: cursor,
			oldEnd:   cursor + taken,
			newLen:   written,
		})
		cursor += taken + 1
	}

	return built
}

// TestASurvivingLineTracesToItself is the property the package exists for: a line the later
// commit did not write over is found again by its content.
func TestASurvivingLineTracesToItself(t *testing.T) {
	random := rand.New(rand.NewSource(20260907))

	const fileLines = 60

	old := make([]string, fileLines)
	for index := range old {
		old[index] = fmt.Sprintf("original %d", index+1)
	}

	for round := range 200 {
		edit := buildScript(random, fileLines)
		next := edit.apply(old)

		positions := make([]Position, 0, fileLines)
		wanted := make([]string, 0, fileLines)
		for line := 1; line <= fileLines; line++ {
			if edit.doesTouch(line) {
				continue
			}

			positions = append(positions, Position{Path: "a.go", Lines: lines(line, line+1)})
			wanted = append(wanted, old[line-1])
		}

		results := Transform(TransformParams{
			Positions: positions,
			Mappings:  edit.mappings(),
			Policy:    OmitOnConflict,
		})
		require.Len(t, results, len(positions), "round %d answered for the wrong number of positions", round)

		for index, result := range results {
			require.Equal(t, Placed, result.Outcome,
				"round %d dropped an untouched line %d", round, positions[index].Lines.Start)
			require.True(t, result.Position.Lines.IsValid(),
				"round %d placed line %d as %v", round, positions[index].Lines.Start, result.Position.Lines)
			require.GreaterOrEqual(t, result.Position.Lines.Start, 1)
			require.LessOrEqual(t, result.Position.Lines.Start, len(next),
				"round %d placed line %d past the end of the file", round, positions[index].Lines.Start)
			assert.Equal(t, wanted[index], next[result.Position.Lines.Start-1],
				"round %d moved line %d onto the wrong content", round, positions[index].Lines.Start)
		}
	}
}

// doesSwallowGap reports whether the script rewrote a region holding this gap strictly inside it,
// which is a gap the later file has no place for.
func (s script) doesSwallowGap(gap int) bool {
	return slices.ContainsFunc(s, func(current replacement) bool {
		return current.oldStart < gap && gap < current.oldEnd
	})
}

// TestAnInsertionPointIsPlacedOrRefused covers the zero width position the package supports and
// the tests above never build: a point between two lines is either carried to a real gap or
// refused, and never placed as a range no file could hold.
func TestAnInsertionPointIsPlacedOrRefused(t *testing.T) {
	random := rand.New(rand.NewSource(4088))

	const fileLines = 50

	for round := range 200 {
		edit := buildScript(random, fileLines)

		for gap := 1; gap <= fileLines+1; gap++ {
			results := Transform(TransformParams{
				Positions: []Position{{Path: "a.go", Lines: lines(gap, gap)}},
				Mappings:  edit.mappings(),
				Policy:    OmitOnConflict,
			})
			require.Len(t, results, 1)

			if edit.doesSwallowGap(gap) {
				assert.Equal(t, Dropped, results[0].Outcome,
					"round %d placed gap %d that a rewritten region swallowed", round, gap)

				continue
			}

			require.Equal(t, Placed, results[0].Outcome,
				"round %d refused gap %d that nothing swallowed", round, gap)
			assert.True(t, results[0].Position.Lines.IsEmpty(),
				"round %d turned gap %d into a range of lines", round, gap)
			assert.True(t, results[0].Position.Lines.IsValid(),
				"round %d placed gap %d as %v", round, gap, results[0].Position.Lines)
		}
	}
}

// TestEveryPositionIsAnsweredExactlyOnce pins the one thing a caller cannot recover from: a
// position that comes back with no result at all is a comment that vanished.
func TestEveryPositionIsAnsweredExactlyOnce(t *testing.T) {
	random := rand.New(rand.NewSource(7))

	const fileLines = 40

	old := make([]string, fileLines)
	for index := range old {
		old[index] = fmt.Sprintf("original %d", index+1)
	}

	for round := range 200 {
		edit := buildScript(random, fileLines)

		positions := make([]Position, 0, fileLines)
		for line := 1; line <= fileLines; line++ {
			positions = append(positions, Position{Path: "a.go", Lines: lines(line, line+1)})
		}

		for _, policy := range []Policy{OmitOnConflict, FileOnConflict} {
			results := Transform(TransformParams{
				Positions: positions,
				Mappings:  edit.mappings(),
				Policy:    policy,
			})
			require.Len(t, results, len(positions))

			for index, result := range results {
				require.Equal(t, index, result.Index, "round %d answered out of order", round)
				if result.Outcome == Placed {
					require.True(t, result.Position.Lines.IsValid(),
						"round %d placed %v", round, result.Position.Lines)
				}
			}
		}
	}
}

// TestOnlyARewrittenLineConflicts is the other half of the contract: a line the script left
// alone is never refused, and a line it wrote over is never quietly placed.
func TestOnlyARewrittenLineConflicts(t *testing.T) {
	random := rand.New(rand.NewSource(1312))

	const fileLines = 50

	for round := range 200 {
		edit := buildScript(random, fileLines)

		for line := 1; line <= fileLines; line++ {
			results := Transform(TransformParams{
				Positions: []Position{{Path: "a.go", Lines: lines(line, line+1)}},
				Mappings:  edit.mappings(),
				Policy:    FileOnConflict,
			})
			require.Len(t, results, 1)

			if edit.doesTouch(line) {
				assert.Equal(t, FileOnly, results[0].Outcome,
					"round %d placed line %d that was written over", round, line)

				continue
			}

			assert.Equal(t, Placed, results[0].Outcome,
				"round %d refused line %d that nothing touched", round, line)
		}
	}
}
