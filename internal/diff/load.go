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

// ErrNoChanges is a worktree that has not moved off its base, refused rather than answered with
// an empty patch.
var ErrNoChanges = errors.New("the worktree holds no change against the base branch")

// ErrAllExcluded is a worktree that has moved, with every part of it inside a path the workflow
// keeps out of the diff.
var ErrAllExcluded = errors.New("every change in the worktree is in a path this workflow excludes from the diff")

// maxFileBytes is the point past which a file's source is not read, giving up the syntax
// colouring while the patch still shows its hunks.
const maxFileBytes = 2 << 20

// patchReader is the git a patch and its two sides are read through.
type patchReader interface {
	GetRoot(ctx context.Context, dir string) (string, error)
	Diff(ctx context.Context, params git.DiffParams) ([]byte, error)
	ReadFile(ctx context.Context, dir, sha, path string) ([]byte, error)
}

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
// with both sides coming out of commits so the patch is the same bytes however long afterwards.
func Load(ctx context.Context, repo patchReader, params LoadParams) (*Patch, error) {
	root, err := repo.GetRoot(ctx, params.Dir)
	if err != nil {
		return nil, err
	}

	raw, err := repo.Diff(ctx, git.DiffParams{
		Dir:     root,
		FromSHA: params.BaseSHA,
		ToSHA:   params.SnapshotSHA,
		Exclude: params.Exclude,
	})
	if err != nil {
		return nil, err
	}

	files, err := parseFiles(ctx, parseFilesParams{
		Git:         repo,
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
		return nil, explainEmptyDiff(ctx, repo, root, params)
	}

	return &Patch{
		Root:        root,
		BaseSHA:     params.BaseSHA,
		SnapshotSHA: params.SnapshotSHA,
		Files:       files,
	}, nil
}

// explainEmptyDiff says why a round drew nothing, telling a worktree that never moved from one
// whose every change is hidden.
func explainEmptyDiff(ctx context.Context, repo patchReader, root string, params LoadParams) error {
	if len(params.Exclude) == 0 {
		return ErrNoChanges
	}

	raw, err := repo.Diff(ctx, git.DiffParams{
		Dir:     root,
		FromSHA: params.BaseSHA,
		ToSHA:   params.SnapshotSHA,
	})
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return ErrNoChanges
	}

	return ErrAllExcluded
}

// parseFilesParams says which patch to read and which two commits to read each side of it out of.
type parseFilesParams struct {
	// Git is what each side of a file is read through.
	Git patchReader
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
			file.OldSource = readRevision(ctx, params.Git, params.Root, params.BaseSHA, file.OldPath)
			file.NewSource = readRevision(ctx, params.Git, params.Root, params.SnapshotSHA, file.NewPath)
			file.Hunks = buildHunks(current.TextFragments)
		}

		files = append(files, file)
	}

	return files, nil
}

// countDrawnBytes is how much text a file's hunks would put on screen, which decides whether it
// is drawn at all.
func countDrawnBytes(fragments []*gitdiff.TextFragment) int {
	total := 0
	for _, fragment := range fragments {
		for _, line := range fragment.Lines {
			total += len(line.Line)
		}
	}

	return total
}

// modeChange is the line to show for a file whose permissions moved, and nothing for a file
// arriving or leaving.
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
			Lines:    make([]Line, 0, len(fragment.Lines)),
			NewStart: int(fragment.NewPosition),
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

func readRevision(ctx context.Context, repo patchReader, root, revision, path string) string {
	if path == "" {
		return ""
	}

	out, err := repo.ReadFile(ctx, root, revision, path)
	if err != nil || len(out) > maxFileBytes {
		return ""
	}

	return string(out)
}

// filePath reads /dev/null as no file at all.
func filePath(path string) string {
	if path == "/dev/null" {
		return ""
	}

	return path
}
