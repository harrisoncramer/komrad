package diff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/internal/git"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// ErrNoChanges is a worktree that has not moved off its base, which is refused rather than
// answered with an empty patch, since a diff with no files in it is nothing to show anybody.
var ErrNoChanges = errors.New("the worktree holds no change against the base branch")

// ErrAllExcluded is a worktree that has moved, every part of it inside a path the workflow keeps
// out of the diff, which draws nothing for the same reason but is not the same thing and must not
// be reported as one.
var ErrAllExcluded = errors.New("every change in the worktree is in a path this workflow excludes from the diff")

// maxFileBytes is the point past which a file's source is not read, giving up the syntax
// colouring while the patch still shows its hunks.
const maxFileBytes = 2 << 20

// LoadParams says which snapshot to read and what to measure it against.
type LoadParams struct {
	// Dir is anywhere inside the agent's worktree.
	Dir string
	// BaseSHA is the commit the round was measured against.
	BaseSHA string
	// SnapshotSHA is the commit the round froze the worktree as.
	SnapshotSHA string
	// Exclude is the paths and globs to leave out, relative to the repository root.
	Exclude []string
}

// Load reads everything one round holds, measured against the point its branch left the base,
// with both sides coming out of commits so the patch is the same bytes however long afterwards it
// is read.
func Load(ctx context.Context, params LoadParams) (*Patch, error) {
	root, err := git.GetRoot(ctx, params.Dir)
	if err != nil {
		return nil, err
	}

	exclude := git.ExcludePathspecs(params.Exclude)

	raw, err := git.RunBytes(ctx, root, diffArgs(params.BaseSHA, params.SnapshotSHA, exclude)...)
	if err != nil {
		return nil, fmt.Errorf("failed to diff %s against %s: %w", params.BaseSHA, params.SnapshotSHA, err)
	}

	files, err := parseFiles(ctx, parseFilesParams{
		Root:        root,
		BaseSHA:     params.BaseSHA,
		SnapshotSHA: params.SnapshotSHA,
		Raw:         raw,
	})
	if err != nil {
		return nil, err
	}

	// Untracked files are already in the snapshot, so the parsed list is the whole of what the
	// reader would see and an empty one is nothing to show them.
	if len(files) == 0 {
		return nil, explainEmptyDiff(ctx, root, params)
	}

	return &Patch{
		Root:        root,
		BaseSHA:     params.BaseSHA,
		SnapshotSHA: params.SnapshotSHA,
		Files:       files,
	}, nil
}

// explainEmptyDiff says why a round drew nothing, since a worktree that never moved and one whose every
// change is hidden from the diff both draw nothing while meaning opposite things about the agent.
func explainEmptyDiff(ctx context.Context, root string, params LoadParams) error {
	if len(params.Exclude) == 0 {
		return ErrNoChanges
	}

	raw, err := git.RunBytes(ctx, root, diffArgs(params.BaseSHA, params.SnapshotSHA, nil)...)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return ErrNoChanges
	}

	return ErrAllExcluded
}

// diffArgs pins the prefix git writes on the header line, since the parser drops the first
// segment of that name whatever it is, so a user whose own config asks for no prefix or a
// different one would have a real directory eaten off every path in the patch.
func diffArgs(baseSHA, snapshotSHA string, exclude []string) []string {
	args := []string{
		"-c", "core.quotepath=false",
		"-c", "diff.external=",
		"-c", "diff.noprefix=false",
		"-c", "diff.mnemonicPrefix=false",
		"-c", "diff.srcPrefix=a/",
		"-c", "diff.dstPrefix=b/",
		"--no-pager", "diff",
		"--no-ext-diff", "--no-color", "--find-renames", "--unified=3", "--full-index",
		baseSHA, snapshotSHA, "--", ".",
	}

	return append(args, exclude...)
}

// parseFilesParams says which patch to read and which two commits to read each side of it out of.
type parseFilesParams struct {
	// Root is the repository the two commits live in.
	Root string
	// BaseSHA is the commit the round was measured against, holding the old side of every file.
	BaseSHA string
	// SnapshotSHA is the commit the round froze, holding the new side.
	SnapshotSHA string
	// Raw is the patch git wrote between the two.
	Raw []byte
}

func parseFiles(ctx context.Context, params parseFilesParams) ([]File, error) {
	parsed, _, err := gitdiff.Parse(bytes.NewReader(params.Raw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse the diff of %q: %w", params.Root, err)
	}

	files := make([]File, 0, len(parsed))
	for _, current := range parsed {
		file := File{OldPath: filePath(current.OldName), NewPath: filePath(current.NewName)}

		switch {
		case current.IsBinary:
			file.Metadata = append(file.Metadata, "binary file")
		case current.IsNew:
			file.Metadata = append(file.Metadata, "new file")
		case current.IsDelete:
			file.Metadata = append(file.Metadata, "deleted file")
		case current.IsRename:
			file.Metadata = append(file.Metadata, "renamed from "+filePath(current.OldName))
		}

		if mode := modeChange(current); mode != "" {
			file.Metadata = append(file.Metadata, mode)
		}

		switch {
		case current.IsBinary:
			// The patch draws no hunks for a binary, so the blob hashes git names on its index
			// line are what the fingerprint has to hold the mark against.
			file.Content = current.OldOIDPrefix + ".." + current.NewOIDPrefix
		case countDrawnBytes(current.TextFragments) > maxFileBytes:
			file.Metadata = append(file.Metadata, "content omitted: over 2 MiB")
			file.Content = current.OldOIDPrefix + ".." + current.NewOIDPrefix
		default:
			file.OldSource = readRevision(ctx, params.Root, params.BaseSHA, file.OldPath)
			file.NewSource = readRevision(ctx, params.Root, params.SnapshotSHA, file.NewPath)
			file.Hunks = buildHunks(current.TextFragments)
		}

		files = append(files, file)
	}

	return files, nil
}

// countDrawnBytes is how much text a file's hunks would put on screen, which is what decides whether it
// is drawn at all, since a snapshot stages a huge file the same as any other.
func countDrawnBytes(fragments []*gitdiff.TextFragment) int {
	total := 0
	for _, fragment := range fragments {
		for _, line := range fragment.Lines {
			total += len(line.Line)
		}
	}

	return total
}

// modeChange is the line to show for a file whose permissions moved, which git puts in the patch
// with no hunk to hang it off, and is nothing for a file arriving or leaving, since those have
// only ever had the one mode and are already named as new or deleted.
func modeChange(file *gitdiff.File) string {
	if file.OldMode == 0 || file.NewMode == 0 || file.OldMode == file.NewMode {
		return ""
	}

	return fmt.Sprintf("mode %04o to %04o", file.OldMode.Perm(), file.NewMode.Perm())
}

func buildHunks(fragments []*gitdiff.TextFragment) []Hunk {
	hunks := make([]Hunk, 0, len(fragments))
	for _, fragment := range fragments {
		comment := ""
		if fragment.Comment != "" {
			comment = " " + fragment.Comment
		}

		hunk := Hunk{
			Header: fmt.Sprintf("@@ -%d,%d +%d,%d @@%s",
				fragment.OldPosition, fragment.OldLines,
				fragment.NewPosition, fragment.NewLines,
				comment),
			Lines: make([]Line, 0, len(fragment.Lines)),
		}

		oldNumber, newNumber := int(fragment.OldPosition), int(fragment.NewPosition)
		for _, line := range fragment.Lines {
			text := strings.TrimSuffix(line.Line, "\n")

			switch line.Op {
			case gitdiff.OpContext:
				hunk.Lines = append(hunk.Lines, Line{
					Kind:      Context,
					Text:      text,
					OldNumber: oldNumber,
					NewNumber: newNumber,
				})
				oldNumber, newNumber = oldNumber+1, newNumber+1
			case gitdiff.OpAdd:
				hunk.Lines = append(hunk.Lines, Line{
					Kind:      Addition,
					Text:      text,
					NewNumber: newNumber,
				})
				newNumber++
			case gitdiff.OpDelete:
				hunk.Lines = append(hunk.Lines, Line{
					Kind:      Deletion,
					Text:      text,
					OldNumber: oldNumber,
				})
				oldNumber++
			}
		}

		hunks = append(hunks, hunk)
	}

	return hunks
}

func readRevision(ctx context.Context, root, revision, path string) string {
	if path == "" {
		return ""
	}

	out, err := git.ReadFile(ctx, root, revision, path)
	if err != nil || len(out) > maxFileBytes {
		return ""
	}

	return string(out)
}

// filePath reads /dev/null as no file at all, which is the whole of it, since the name arrives
// with the diff prefix already off.
func filePath(path string) string {
	if path == "/dev/null" {
		return ""
	}

	return path
}
