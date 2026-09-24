//go:build integration

package git

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"komrade/internal/position"
	"komrade/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// numbered is a file whose every line says which line it is, so a traced position can be checked
// against content.
func numbered(count int) string {
	lines := make([]string, 0, count)
	for line := 1; line <= count; line++ {
		lines = append(lines, fmt.Sprintf("original %d", line))
	}

	return strings.Join(lines, "\n") + "\n"
}

// commitAll takes everything in the worktree as one commit and answers with its sha.
func commitAll(t *testing.T, root, message string) string {
	t.Helper()

	run(t, root, "add", "-A")
	run(t, root, "commit", "--allow-empty", "-m", message)

	return capture(t, root, "rev-parse", "HEAD")
}

// TestIntegrationMapRangesRefusals covers the callers that hand over something a repository cannot answer
// for.
func TestIntegrationMapRangesRefusals(t *testing.T) {
	root := repo(t)
	head := capture(t, root, "rev-parse", "HEAD")

	tests := []struct {
		name   string
		params MapRangesParams
	}{
		{
			name:   "no commits at all",
			params: MapRangesParams{Dir: root},
		},
		{
			name:   "only the commit being moved from",
			params: MapRangesParams{Dir: root, FromSHA: head},
		},
		{
			name: "a commit that does not exist",
			params: MapRangesParams{
				Dir:     root,
				FromSHA: head,
				ToSHA:   "0000000000000000000000000000000000000000",
			},
		},
		{
			name: "a directory that is not a repository",
			params: MapRangesParams{
				Dir:     t.TempDir(),
				FromSHA: head,
				ToSHA:   head,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mappings, err := client.MapRanges(t.Context(), tt.params)

			require.Error(t, err)
			assert.Nil(t, mappings)
		})
	}
}

// TestIntegrationMapRangesReadsWhatTheCommitDid pins the shape of every change git can write.
func TestIntegrationMapRangesReadsWhatTheCommitDid(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, root string)
		want   []position.Mapping
	}{
		{
			name:   "a commit that touched nothing maps nothing",
			change: func(*testing.T, string) {},
			want:   []position.Mapping{},
		},
		{
			name: "lines arriving are a region of no old lines at the point they arrive",
			change: func(t *testing.T, root string) {
				write(t, root, "counted.go", strings.Replace(numbered(9),
					"original 4\n", "original 4\narrived a\narrived b\n", 1))
			},
			want: []position.Mapping{
				{
					File: position.FileMapping{OldPath: "counted.go", NewPath: "counted.go"},
					Ranges: []position.RangeMapping{
						{
							Old: position.Range{Start: 5, End: 5},
							New: position.Range{Start: 5, End: 7},
						},
					},
				},
			},
		},
		{
			name: "lines leaving are a region of no new lines at the point they left",
			change: func(t *testing.T, root string) {
				write(t, root, "counted.go", strings.Replace(numbered(9),
					"original 4\noriginal 5\n", "", 1))
			},
			want: []position.Mapping{
				{
					File: position.FileMapping{OldPath: "counted.go", NewPath: "counted.go"},
					Ranges: []position.RangeMapping{
						{
							Old: position.Range{Start: 4, End: 6},
							New: position.Range{Start: 4, End: 4},
						},
					},
				},
			},
		},
		{
			name: "a line rewritten in place is one region on both sides",
			change: func(t *testing.T, root string) {
				write(t, root, "counted.go", strings.Replace(numbered(9),
					"original 6\n", "rewritten 6\n", 1))
			},
			want: []position.Mapping{
				{
					File: position.FileMapping{OldPath: "counted.go", NewPath: "counted.go"},
					Ranges: []position.RangeMapping{
						{
							Old: position.Range{Start: 6, End: 7},
							New: position.Range{Start: 6, End: 7},
						},
					},
				},
			},
		},
		{
			name: "a file arriving has no old path",
			change: func(t *testing.T, root string) {
				write(t, root, "arrived.go", "package main\n")
			},
			want: []position.Mapping{
				{
					File: position.FileMapping{OldPath: "", NewPath: "arrived.go"},
					Ranges: []position.RangeMapping{
						{
							Old: position.Range{Start: 1, End: 1},
							New: position.Range{Start: 1, End: 2},
						},
					},
				},
			},
		},
		{
			name: "a file leaving has no new path",
			change: func(t *testing.T, root string) {
				require.NoError(t, os.Remove(filepath.Join(root, "counted.go")))
			},
			want: []position.Mapping{
				{
					File: position.FileMapping{OldPath: "counted.go", NewPath: ""},
					Ranges: []position.RangeMapping{
						{
							Old: position.Range{Start: 1, End: 10},
							New: position.Range{Start: 1, End: 1},
						},
					},
				},
			},
		},
		{
			name: "a rename carries the path across with its edits",
			change: func(t *testing.T, root string) {
				require.NoError(t, os.Remove(filepath.Join(root, "counted.go")))
				write(t, root, "renamed.go", strings.Replace(numbered(9),
					"original 2\n", "rewritten 2\n", 1))
			},
			want: []position.Mapping{
				{
					File: position.FileMapping{OldPath: "counted.go", NewPath: "renamed.go"},
					Ranges: []position.RangeMapping{
						{
							Old: position.Range{Start: 2, End: 3},
							New: position.Range{Start: 2, End: 3},
						},
					},
				},
			},
		},
		{
			name: "a rename that changed nothing else maps the path and no regions",
			change: func(t *testing.T, root string) {
				run(t, root, "mv", "counted.go", "moved.go")
			},
			want: []position.Mapping{
				{
					File:   position.FileMapping{OldPath: "counted.go", NewPath: "moved.go"},
					Ranges: []position.RangeMapping{},
				},
			},
		},
		{
			name: "a change of mode alone maps no regions",
			change: func(t *testing.T, root string) {
				require.NoError(t, os.Chmod(filepath.Join(root, "counted.go"), 0o755))
			},
			want: []position.Mapping{
				{
					File:   position.FileMapping{OldPath: "counted.go", NewPath: "counted.go"},
					Ranges: []position.RangeMapping{},
				},
			},
		},
		{
			name: "a binary maps no regions since a patch draws none of it",
			change: func(t *testing.T, root string) {
				require.NoError(t, os.WriteFile(filepath.Join(root, "blob.bin"),
					[]byte{0x00, 0x01, 0x02, 0x00}, 0o644))
			},
			want: []position.Mapping{
				{
					File:   position.FileMapping{OldPath: "", NewPath: "blob.bin"},
					Ranges: nil,
				},
			},
		},
		{
			name: "a last line losing its newline is still one region",
			change: func(t *testing.T, root string) {
				write(t, root, "counted.go", strings.TrimSuffix(numbered(9), "\n"))
			},
			want: []position.Mapping{
				{
					File: position.FileMapping{OldPath: "counted.go", NewPath: "counted.go"},
					Ranges: []position.RangeMapping{
						{
							Old: position.Range{Start: 9, End: 10},
							New: position.Range{Start: 9, End: 10},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := repo(t)
			write(t, root, "counted.go", numbered(9))
			from := commitAll(t, root, "counted")

			tt.change(t, root)
			to := commitAll(t, root, "changed")

			mappings, err := client.MapRanges(t.Context(), MapRangesParams{
				Dir:     root,
				FromSHA: from,
				ToSHA:   to,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.want, mappings)
		})
	}
}

// TestIntegrationMapRangesFindsARenameAcrossTheWholeTree pins why the whole tree is read: git applies a
// pathspec before it looks for renames, so a diff filtered to a file's old path reports that file
// deleted and every position on it is lost.
func TestIntegrationMapRangesFindsARenameAcrossTheWholeTree(t *testing.T) {
	root := repo(t)
	write(t, root, "old.go", numbered(9))
	from := commitAll(t, root, "before the rename")

	require.NoError(t, os.Remove(filepath.Join(root, "old.go")))
	write(t, root, "new.go", strings.Replace(numbered(9), "original 2\n", "rewritten 2\n", 1))
	to := commitAll(t, root, "after the rename")

	mappings, err := client.MapRanges(t.Context(), MapRangesParams{
		Dir:     root,
		FromSHA: from,
		ToSHA:   to,
	})
	require.NoError(t, err)

	require.Len(t, mappings, 1)
	assert.Equal(t, "old.go", mappings[0].File.OldPath)
	assert.Equal(t, "new.go", mappings[0].File.NewPath, "a rename read as a deletion drops every position on the file")
}

// TestIntegrationATracedLineHoldsItsContent proves the hunk convention read here is the one git writes: a
// line a real commit left alone is found again by its content.
func TestIntegrationATracedLineHoldsItsContent(t *testing.T) {
	random := rand.New(rand.NewSource(20260907))

	const fileLines = 40

	for round := range 40 {
		root := repo(t)
		write(t, root, "counted.go", numbered(fileLines))
		from := commitAll(t, root, "counted")

		kept, next := rewriteSomeLines(random, fileLines)
		write(t, root, "counted.go", next)
		to := commitAll(t, root, "rewritten")

		mappings, err := client.MapRanges(t.Context(), MapRangesParams{
			Dir:     root,
			FromSHA: from,
			ToSHA:   to,
		})
		require.NoError(t, err)

		positions := xslice.Map(kept, func(line int) position.Position {
			return position.Position{
				Path:  "counted.go",
				Lines: position.Range{Start: line, End: line + 1},
			}
		})

		results := position.Transform(position.TransformParams{
			Positions: positions,
			Mappings:  mappings,
			Policy:    position.OmitOnConflict,
		})
		require.Len(t, results, len(positions))

		written := strings.Split(strings.TrimSuffix(next, "\n"), "\n")
		for index, result := range results {
			require.Equal(t, position.Placed, result.Outcome,
				"round %d refused line %d, which nothing touched", round, kept[index])
			require.LessOrEqual(t, result.Position.Lines.Start, len(written))
			assert.Equal(t, fmt.Sprintf("original %d", kept[index]),
				written[result.Position.Lines.Start-1],
				"round %d moved line %d onto the wrong content", round, kept[index])
		}
	}
}

// rewriteSomeLines is a file rewritten at random and the original lines it left alone.
func rewriteSomeLines(random *rand.Rand, count int) ([]int, string) {
	kept := make([]int, 0, count)
	written := make([]string, 0, count)

	for line := 1; line <= count; line++ {
		switch random.Intn(4) {
		case 0:
			written = append(written, fmt.Sprintf("rewritten %d", line))
		case 1:
			written = append(written,
				fmt.Sprintf("arrived above %d", line),
				fmt.Sprintf("original %d", line))
			kept = append(kept, line)
		case 2:
			continue
		default:
			written = append(written, fmt.Sprintf("original %d", line))
			kept = append(kept, line)
		}
	}

	return kept, strings.Join(written, "\n") + "\n"
}
