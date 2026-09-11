package git

import (
	"bytes"
	"context"
	"fmt"

	"weave/internal/position"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// MapRangesParams says which two commits to read.
type MapRangesParams struct {
	// Dir is anywhere inside the repository holding both commits.
	Dir string
	// FromSHA is the commit the positions being moved were measured in.
	FromSHA string
	// ToSHA is the commit they are being moved to.
	ToSHA string
}

// MapRanges is what the later commit did to each file, in the form the position package moves a
// place through.
func (c *Client) MapRanges(ctx context.Context, params MapRangesParams) ([]position.Mapping, error) {
	if params.FromSHA == "" || params.ToSHA == "" {
		return nil, fmt.Errorf("failed to map %q: two commits are needed", params.Dir)
	}

	root, err := c.GetRoot(ctx, params.Dir)
	if err != nil {
		return nil, err
	}

	// Explainer: the diff is read with no context lines. With git's usual three, a hunk claims
	// the untouched lines around a change as part of it, and a position on one of those lines
	// comes back reported as written over when nothing touched it.
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

// mapPatch reads a patch git has already written.
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

// mapRangesArgs is the diff the mappings are read from.
func mapRangesArgs(params MapRangesParams) []string {
	// Explainer: the prefixes git writes on the header lines are pinned here. The parser drops
	// the first segment of every path whatever it is, so a repository configured to write bare
	// paths would have a real directory eaten off the front of each one. The whole tree is read
	// rather than one path, because git applies a pathspec before it looks for renames, so
	// filtering by a file's old path reports it deleted rather than renamed.
	args := []string{
		"-c", "core.quotepath=false",
		"-c", "diff.external=",
		"-c", "diff.noprefix=false",
		"-c", "diff.mnemonicPrefix=false",
		"-c", "diff.srcPrefix=a/",
		"-c", "diff.dstPrefix=b/",
		"--no-pager", "diff",
		"--no-ext-diff", "--no-color", "--find-renames", "--unified=0", "--full-index",
		params.FromSHA, params.ToSHA, "--", ".",
	}

	return args
}

// rangesOf is the regions of one file the later commit rewrote, and nothing for a file the patch
// draws no content for.
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

// filePath reads /dev/null as no file at all.
func filePath(path string) string {
	if path == "/dev/null" {
		return ""
	}

	return path
}

// rangeOf turns one side of a hunk header into a range, counting a hunk of no lines as the point
// they arrive at.
func rangeOf(start, count int64) position.Range {
	if count == 0 {
		return position.Range{Start: int(start) + 1, End: int(start) + 1}
	}

	return position.Range{Start: int(start), End: int(start + count)}
}
