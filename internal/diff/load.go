package diff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

const gitTimeout = 30 * time.Second

// ErrNoChanges is a worktree that has not moved off its base, which is refused rather than
// answered with an empty patch, since a diff with no files in it is nothing to show anybody.
var ErrNoChanges = errors.New("the worktree holds no change against the base branch")

// maxFileBytes is the point past which a file's source is not read, giving up the syntax
// colouring while the patch still shows its hunks.
const maxFileBytes = 2 << 20

// LoadParams says which worktree to read and what to measure it against.
type LoadParams struct {
	// Dir is anywhere inside the agent's worktree.
	Dir string
	// Base is the branch the worktree was cut from, or empty to ask the repository for its
	// default branch.
	Base string
	// Exclude is the paths and globs to leave out, relative to the repository root.
	Exclude []string
}

// Load reads everything an agent has changed in its worktree, committed and uncommitted
// alike, measured against the point its branch left the base.
func Load(ctx context.Context, params LoadParams) (*Patch, error) {
	root, err := runGit(ctx, params.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("failed to find the repository at %q: %w", params.Dir, err)
	}

	base := resolveBase(ctx, root, params.Base)

	commit, err := runGit(ctx, root, "merge-base", base, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to find where %q left %q: %w", root, base, err)
	}

	exclude := excludePathspecs(params.Exclude)

	raw, err := runGitBytes(ctx, root, diffArgs(commit, exclude)...)
	if err != nil {
		return nil, fmt.Errorf("failed to diff %q against %s: %w", root, commit, err)
	}

	files, err := parseFiles(ctx, root, commit, raw)
	if err != nil {
		return nil, err
	}

	// Untracked files are added on top, since they are in no diff git will produce.
	untracked, err := loadUntracked(ctx, root, exclude)
	if err != nil {
		return nil, err
	}

	files = append(files, untracked...)
	if len(files) == 0 {
		return nil, ErrNoChanges
	}

	return &Patch{
		Root:   root,
		Base:   base,
		Commit: commit,
		Files:  files,
	}, nil
}

// diffArgs pins the prefix git writes on the header line, since the parser drops the first
// segment of that name whatever it is, so a user whose own config asks for no prefix or a
// different one would have a real directory eaten off every path in the patch.
func diffArgs(commit string, exclude []string) []string {
	args := []string{
		"-c", "core.quotepath=false",
		"-c", "diff.external=",
		"-c", "diff.noprefix=false",
		"-c", "diff.mnemonicPrefix=false",
		"-c", "diff.srcPrefix=a/",
		"-c", "diff.dstPrefix=b/",
		"--no-pager", "diff",
		"--no-ext-diff", "--no-color", "--find-renames", "--unified=3", "--full-index",
		commit, "--", ".",
	}

	return append(args, exclude...)
}

// excludePathspecs turns the workflow's paths into pathspecs git will subtract, adding the
// exclusion magic so the user writes only a path or a glob such as proto/gen.
func excludePathspecs(paths []string) []string {
	specs := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		specs = append(specs, ":(exclude)"+path)
	}

	return specs
}

// resolveBase turns a configured base branch into something git can name, preferring a
// remote-tracking ref since the worktree was cut from one.
func resolveBase(ctx context.Context, root, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		if head, err := runGit(ctx, root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
			return head
		}

		return "HEAD"
	}

	if _, err := runGit(ctx, root, "rev-parse", "--verify", "--quiet", "origin/"+base); err == nil {
		return "origin/" + base
	}

	return base
}

func parseFiles(ctx context.Context, root, commit string, raw []byte) ([]File, error) {
	parsed, _, err := gitdiff.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse the diff of %q: %w", root, err)
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

		if current.IsBinary {
			// The patch draws no hunks for a binary, so the blob hashes git names on its index
			// line are what the fingerprint has to hold the mark against.
			file.Content = current.OldOIDPrefix + ".." + current.NewOIDPrefix
		} else {
			file.OldSource = readRevision(ctx, root, commit, file.OldPath)
			file.NewSource = readWorkingTree(root, file.NewPath)
			file.Hunks = buildHunks(current.TextFragments)
		}

		files = append(files, file)
	}

	return files, nil
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

// loadUntracked builds an add-only file for everything git is not tracking yet, since an
// agent that has written a new file and not staged it has done work no diff will show.
func loadUntracked(ctx context.Context, root string, exclude []string) ([]File, error) {
	args := append([]string{"ls-files", "--others", "--exclude-standard", "-z", "--", "."}, exclude...)

	out, err := runGitBytes(ctx, root, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list untracked files in %q: %w", root, err)
	}

	var files []File
	for _, raw := range bytes.Split(out, []byte{0}) {
		if len(raw) == 0 {
			continue
		}

		path := string(raw)
		file := File{NewPath: path, Metadata: []string{"untracked file"}}

		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		if info.Size() > maxFileBytes {
			file.Metadata = append(file.Metadata, "content omitted: over 2 MiB")
			file.Content = hashObject(ctx, root, path)
			files = append(files, file)

			continue
		}

		content := readWorkingTree(root, path)
		if strings.IndexByte(content, 0) >= 0 {
			file.Metadata = []string{"untracked binary file"}
			file.Content = hashObject(ctx, root, path)
			files = append(files, file)

			continue
		}

		file.NewSource = content
		file.Hunks = []Hunk{addedHunk(content)}
		files = append(files, file)
	}

	return files, nil
}

func addedHunk(content string) Hunk {
	sources := strings.Split(strings.TrimSuffix(content, "\n"), "\n")

	lines := make([]Line, 0, len(sources))
	for index, text := range sources {
		lines = append(lines, Line{
			Kind:      Addition,
			Text:      text,
			NewNumber: index + 1,
		})
	}

	return Hunk{Header: fmt.Sprintf("@@ -0,0 +1,%d @@", len(lines)), Lines: lines}
}

// hashObject is what git would call the file on disk, which is how an untracked file the patch
// draws nothing of still has something that moves when the agent rewrites it, and it reads the
// file rather than the index, so it serves one no revision holds and one too big to load alike.
func hashObject(ctx context.Context, root, path string) string {
	oid, err := runGit(ctx, root, "hash-object", "--", path)
	if err != nil {
		return ""
	}

	return oid
}

func readRevision(ctx context.Context, root, revision, path string) string {
	if path == "" {
		return ""
	}

	out, err := runGitBytes(ctx, root, "show", revision+":"+path)
	if err != nil || len(out) > maxFileBytes {
		return ""
	}

	return string(out)
}

func readWorkingTree(root, path string) string {
	if path == "" {
		return ""
	}

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil || len(content) > maxFileBytes {
		return ""
	}

	return string(content)
}

// filePath reads /dev/null as no file at all, which is the whole of it, since the name arrives
// with the diff prefix already off.
func filePath(path string) string {
	if path == "/dev/null" {
		return ""
	}

	return path
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := runGitBytes(ctx, dir, args...)

	return strings.TrimSpace(string(out)), err
}

func runGitBytes(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return out, nil
}
