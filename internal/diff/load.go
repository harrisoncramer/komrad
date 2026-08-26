package diff

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

const gitTimeout = 30 * time.Second

// maxFileBytes is the point past which a file's source is not read for highlighting. The
// patch still shows its hunks; only the syntax colouring is given up.
const maxFileBytes = 2 << 20

// LoadParams says which worktree to read and what to measure it against.
type LoadParams struct {
	// Dir is anywhere inside the agent's worktree.
	Dir string
	// Base is the branch the worktree was cut from. Empty means ask the repository for its
	// default branch.
	Base string
}

// Load reads everything an agent has changed in its worktree, measured against the point its
// branch left the base. Committed and uncommitted work arrive together, because a diff
// against a commit compares the working tree to it, and untracked files are added on top,
// since they are in no diff git will produce.
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

	raw, err := runGitBytes(ctx, root, diffArgs(commit)...)
	if err != nil {
		return nil, fmt.Errorf("failed to diff %q against %s: %w", root, commit, err)
	}

	files, err := parseFiles(ctx, root, commit, raw)
	if err != nil {
		return nil, err
	}

	untracked, err := loadUntracked(ctx, root)
	if err != nil {
		return nil, err
	}

	return &Patch{Root: root, Base: base, Commit: commit, Files: append(files, untracked...)}, nil
}

func diffArgs(commit string) []string {
	return []string{
		"-c", "core.quotepath=false",
		"-c", "diff.external=",
		"--no-pager", "diff",
		"--no-ext-diff", "--no-color", "--find-renames", "--unified=3",
		commit, "--",
	}
}

// resolveBase turns a configured base branch into something git can name. A remote-tracking
// ref is preferred, since the worktree was cut from one, and a repository that names no base
// at all falls back to whatever origin points its HEAD at.
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
		file := File{OldPath: cleanPath(current.OldName), NewPath: cleanPath(current.NewName)}

		switch {
		case current.IsBinary:
			file.Metadata = append(file.Metadata, "binary file")
		case current.IsNew:
			file.Metadata = append(file.Metadata, "new file")
		case current.IsDelete:
			file.Metadata = append(file.Metadata, "deleted file")
		case current.IsRename:
			file.Metadata = append(file.Metadata, "renamed from "+cleanPath(current.OldName))
		}

		if !current.IsBinary {
			file.OldSource = readRevision(ctx, root, commit, file.OldPath)
			file.NewSource = readWorkingTree(root, file.NewPath)
			file.Hunks = buildHunks(current.TextFragments)
		}

		files = append(files, file)
	}

	return files, nil
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
				hunk.Lines = append(hunk.Lines, Line{Kind: Context, Text: text, OldNumber: oldNumber, NewNumber: newNumber})
				oldNumber, newNumber = oldNumber+1, newNumber+1
			case gitdiff.OpAdd:
				hunk.Lines = append(hunk.Lines, Line{Kind: Addition, Text: text, NewNumber: newNumber})
				newNumber++
			case gitdiff.OpDelete:
				hunk.Lines = append(hunk.Lines, Line{Kind: Deletion, Text: text, OldNumber: oldNumber})
				oldNumber++
			}
		}

		hunks = append(hunks, hunk)
	}

	return hunks
}

// loadUntracked builds an add-only file for everything git is not tracking yet, since an
// agent that has written a new file and not staged it has done work no diff will show.
func loadUntracked(ctx context.Context, root string) ([]File, error) {
	out, err := runGitBytes(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
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
			files = append(files, file)

			continue
		}

		content := readWorkingTree(root, path)
		if strings.IndexByte(content, 0) >= 0 {
			file.Metadata = []string{"untracked binary file"}
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
		lines = append(lines, Line{Kind: Addition, Text: text, NewNumber: index + 1})
	}

	return Hunk{Header: fmt.Sprintf("@@ -0,0 +1,%d @@", len(lines)), Lines: lines}
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

// cleanPath drops the a/ and b/ prefixes git writes, and reads /dev/null as no file at all.
func cleanPath(path string) string {
	if path == "" || path == "/dev/null" {
		return ""
	}

	for _, prefix := range []string{"a/", "b/"} {
		if strings.HasPrefix(path, prefix) {
			return strings.TrimPrefix(path, prefix)
		}
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
