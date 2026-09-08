package git

import (
	"bytes"
	"context"
	"fmt"

	"weave/internal/position"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// MapRangesParams says which two commits to read and, when the caller only cares about some of
// the tree, which paths to read of them.
type MapRangesParams struct {
	// Dir is anywhere inside the repository holding both commits.
	Dir string
	// FromSHA is the commit the positions being moved were measured in.
	FromSHA string
	// ToSHA is the commit they are being moved to.
	ToSHA string
	// Paths are the files to read, in the coordinates of the earlier commit, or none for the
	// whole tree.
	Paths []string
}

// MapRanges is what the later commit did to each file, in the form the position package moves a
// place through, read with no context lines at all so that a region is only the lines that
// actually changed and a comment beside one of them is not reported as written over.
func MapRanges(ctx context.Context, params MapRangesParams) ([]position.Mapping, error) {
	if params.FromSHA == "" || params.ToSHA == "" {
		return nil, fmt.Errorf("failed to map %q: two commits are needed", params.Dir)
	}

	root, err := GetRoot(ctx, params.Dir)
	if err != nil {
		return nil, err
	}

	raw, err := RunBytes(ctx, root, mapRangesArgs(params)...)
	if err != nil {
		return nil, fmt.Errorf("failed to diff %s against %s: %w", params.FromSHA, params.ToSHA, err)
	}

	mappings, err := mapPatch(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to read the diff of %s against %s: %w", params.FromSHA, params.ToSHA, err)
	}

	return mappings, nil
}

// mapPatch reads a patch git has already written, kept apart from running git so that a fuzzer
// can hand it anything at all.
func mapPatch(raw []byte) ([]position.Mapping, error) {
	files, _, err := gitdiff.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse a patch: %w", err)
	}

	mappings := make([]position.Mapping, 0, len(files))
	for _, file := range files {
		mappings = append(mappings, position.Mapping{
			File: position.FileMapping{
				OldPath: filePath(file.OldName),
				NewPath: filePath(file.NewName),
			},
			Ranges: rangesOf(file),
		})
	}

	return mappings, nil
}

// mapRangesArgs pins the prefixes git writes on the header lines, since the parser drops the
// first segment of a name whatever it is, so a user whose own config asks for no prefix would
// have a real directory eaten off every path.
func mapRangesArgs(params MapRangesParams) []string {
	args := []string{
		"-c", "core.quotepath=false",
		"-c", "diff.external=",
		"-c", "diff.noprefix=false",
		"-c", "diff.mnemonicPrefix=false",
		"-c", "diff.srcPrefix=a/",
		"-c", "diff.dstPrefix=b/",
		"--no-pager", "diff",
		"--no-ext-diff", "--no-color", "--find-renames", "--unified=0", "--full-index",
		params.FromSHA, params.ToSHA, "--",
	}

	if len(params.Paths) == 0 {
		return append(args, ".")
	}

	return append(args, params.Paths...)
}

// rangesOf is the regions of one file the later commit rewrote, and nothing at all for a file
// whose content the patch does not draw, which is a binary, a rename that changed nothing, and a
// file whose only change was its mode.
func rangesOf(file *gitdiff.File) []position.RangeMapping {
	if file.IsBinary {
		return nil
	}

	ranges := make([]position.RangeMapping, 0, len(file.TextFragments))
	for _, fragment := range file.TextFragments {
		ranges = append(ranges, position.RangeMapping{
			Old: rangeOf(fragment.OldPosition, fragment.OldLines),
			New: rangeOf(fragment.NewPosition, fragment.NewLines),
		})
	}

	return ranges
}

// filePath reads /dev/null as no file at all, which is how a patch names the missing side of a
// file arriving or leaving.
func filePath(path string) string {
	if path == "/dev/null" {
		return ""
	}

	return path
}

// rangeOf turns one side of a hunk header into a range, reading a hunk that covers no lines as
// the point they would arrive at, which git numbers as the line before it rather than the line
// it sits in front of.
func rangeOf(start, count int64) position.Range {
	if count == 0 {
		return position.Range{Start: int(start) + 1, End: int(start) + 1}
	}

	return position.Range{Start: int(start), End: int(start + count)}
}
