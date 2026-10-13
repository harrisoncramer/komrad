package git

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"komrad/internal/position"
	"komrad/lib/xslice"

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
	// Paths limits the answer to these files of FromSHA, and every file reads when it is empty.
	Paths []string
}

// MapRanges is what the later commit did to each file, in the form the position package moves a
// place through.
func (c *Client) MapRanges(ctx context.Context, params MapRangesParams) ([]position.Mapping, error) {
	if params.FromSHA == "" || params.ToSHA == "" {
		return nil, fmt.Errorf("failed to map %q: two commits are needed", params.Dir)
	}

	root, err := getRoot(ctx, params.Dir)
	if err != nil {
		return nil, err
	}

	pathspecs := []string{"."}
	if len(params.Paths) > 0 {
		pathspecs, err = changedPathspecs(ctx, root, params)
		if err != nil {
			return nil, err
		}

		if len(pathspecs) == 0 {
			return []position.Mapping{}, nil
		}
	}

	raw, err := runBytes(ctx, root, mapRangesArgs(params, pathspecs)...)
	if err != nil {
		return nil, fmt.Errorf("failed to diff %s against %s: %w", params.FromSHA, params.ToSHA, err)
	}

	mappings, err := mapPatch(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to read the diff of %s against %s: %w", params.FromSHA, params.ToSHA, err)
	}

	return mappings, nil
}

// changedPathspecs is every path the later commit gave the files asked for, old and new names
// both, read from a diff of names alone so renames are found across the whole tree.
func changedPathspecs(ctx context.Context, root string, params MapRangesParams) ([]string, error) {
	raw, err := runBytes(ctx, root,
		"-c", "core.quotepath=false",
		"--no-pager", "diff",
		"--no-ext-diff", "--no-color", "--find-renames", "--name-status", "-z",
		params.FromSHA, params.ToSHA, "--", ".",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list what %s changed against %s: %w", params.ToSHA, params.FromSHA, err)
	}

	changes, err := parseNameStatus(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to read what %s changed against %s: %w", params.ToSHA, params.FromSHA, err)
	}

	var pathspecs []string
	for _, change := range changes {
		if !slices.Contains(params.Paths, change.oldPath) {
			continue
		}

		pathspecs = append(pathspecs, ":(literal)"+change.oldPath)
		if change.newPath != change.oldPath {
			pathspecs = append(pathspecs, ":(literal)"+change.newPath)
		}
	}

	return pathspecs, nil
}

// nameChange is one entry of a diff of names: the path a file had before and the path it has after.
type nameChange struct {
	oldPath string
	newPath string
}

// parseNameStatus reads the output of diff --name-status -z.
func parseNameStatus(raw []byte) ([]nameChange, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	fields := bytes.Split(bytes.TrimSuffix(raw, []byte{0}), []byte{0})

	var changes []nameChange
	for i := 0; i < len(fields); {
		status := fields[i]
		if len(status) == 0 {
			return nil, fmt.Errorf("an empty status at field %d", i)
		}

		paths := 1
		if status[0] == 'R' || status[0] == 'C' {
			paths = 2
		}

		if i+paths >= len(fields) {
			return nil, fmt.Errorf("status %q at field %d names too few paths", status, i)
		}

		change := nameChange{oldPath: string(fields[i+1]), newPath: string(fields[i+paths])}
		changes = append(changes, change)
		i += 1 + paths
	}

	return changes, nil
}

// mapPatch reads a patch git has already written.
func mapPatch(raw []byte) ([]position.Mapping, error) {
	files, _, err := gitdiff.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse a patch: %w", err)
	}

	mappings := xslice.Map(files, func(file *gitdiff.File) position.Mapping {
		return position.Mapping{
			File: position.FileMapping{
				OldPath: FilePath(file.OldName),
				NewPath: FilePath(file.NewName),
			},
			Ranges: rangesOf(file),
		}
	})

	return mappings, nil
}

// mapRangesArgs is the diff the mappings are read from.
func mapRangesArgs(params MapRangesParams, pathspecs []string) []string {
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

	return append(args, pathspecs...)
}

// rangesOf is the regions of one file the later commit rewrote, and nothing for a file the patch
// draws no content for.
func rangesOf(file *gitdiff.File) []position.RangeMapping {
	if file.IsBinary {
		return nil
	}

	ranges := xslice.Map(file.TextFragments, func(fragment *gitdiff.TextFragment) position.RangeMapping {
		return position.RangeMapping{
			Old: rangeOf(fragment.OldPosition, fragment.OldLines),
			New: rangeOf(fragment.NewPosition, fragment.NewLines),
		}
	})

	return ranges
}

// FilePath reads /dev/null as no file at all.
func FilePath(path string) string {
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
