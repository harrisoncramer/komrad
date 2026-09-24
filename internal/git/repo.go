package git

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"komrade/internal/fail"
)

// GetRoot is the top of the working tree any path inside it belongs to.
func (c *Client) GetRoot(ctx context.Context, dir string) (string, error) {
	return getRoot(ctx, local, dir)
}

func getRoot(ctx context.Context, on runner, dir string) (string, error) {
	root, err := runOn(ctx, on, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("failed to find the repository at %q: %w", dir, err)
	}

	return root, nil
}

// ErrBaseMissing says the workflow's base branch is not a ref this clone holds.
var ErrBaseMissing = fail.Precondition("the base branch is not one the clone has")

// resolveBase is the ref a named base is measured as: origin's copy when the clone has one,
// else the local branch.
func resolveBase(ctx context.Context, root, base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("%w: the workflow names no base branch", ErrBaseMissing)
	}

	if _, err := Run(ctx, root, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+base); err == nil {
		return "origin/" + base, nil
	}

	if _, err := Run(ctx, root, "rev-parse", "--verify", "--quiet", "refs/heads/"+base); err == nil {
		return "refs/heads/" + base, nil
	}

	return "", fmt.Errorf("%w: the clone has no origin/%s and no branch %s; set the workflow's base branch to one it has", ErrBaseMissing, base, base)
}

// DefaultBase is the branch a repository would name as its base when nobody has: what the
// clone's origin/HEAD points at, else the branch the checkout is on, else main.
func DefaultBase(ctx context.Context, dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "main"
	}

	head, err := Run(ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		if name, ok := strings.CutPrefix(head, "origin/"); ok && name != "" {
			return name
		}
	}

	branch, err := Run(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil && branch != "" {
		return branch
	}

	return "main"
}

// GetCurrentBranch is the branch a worktree is on, and nothing for a detached head or a
// directory git will not answer about.
func (c *Client) GetCurrentBranch(ctx context.Context, dir string) *string {
	branch, err := Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" || branch == "HEAD" {
		return nil
	}

	return &branch
}

// ReadFile is one file as a commit holds it.
func (c *Client) ReadFile(ctx context.Context, dir, sha, path string) ([]byte, error) {
	out, err := RunBytes(ctx, dir, "show", sha+":"+path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q at %q: %w", path, sha, err)
	}

	return out, nil
}

// ReadFileLines is one file as a commit holds it, split into lines.
func (c *Client) ReadFileLines(ctx context.Context, dir, sha, path string) ([]string, error) {
	out, err := c.ReadFile(ctx, dir, sha, path)
	if err != nil {
		return nil, err
	}

	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"), nil
}

// FileRevision names one file as one commit holds it.
type FileRevision struct {
	SHA  string
	Path string
}

// ReadFiles is every named file as its commit holds it, read in one command, with a file the
// commit does not hold left out.
func (c *Client) ReadFiles(ctx context.Context, dir string, revisions []FileRevision) (map[FileRevision][]byte, error) {
	return readFiles(ctx, local, dir, revisions)
}

func readFiles(ctx context.Context, on runner, dir string, revisions []FileRevision) (map[FileRevision][]byte, error) {
	files := make(map[FileRevision][]byte, len(revisions))
	if len(revisions) == 0 {
		return files, nil
	}

	var input bytes.Buffer
	for _, revision := range revisions {
		input.WriteString(revision.SHA + ":" + revision.Path + "\n")
	}

	out, err := runInput(ctx, on, dir, nil, input.Bytes(), "cat-file", "--batch")
	if err != nil {
		return nil, fmt.Errorf("failed to read %d files of %q: %w", len(revisions), dir, err)
	}

	for _, revision := range revisions {
		header, rest, found := bytes.Cut(out, []byte{'\n'})
		if !found {
			return nil, fmt.Errorf("failed to read %q at %q: git answered nothing", revision.Path, revision.SHA)
		}

		out = rest

		if bytes.HasSuffix(header, []byte(" missing")) || bytes.HasSuffix(header, []byte(" ambiguous")) {
			continue
		}

		fields := strings.Fields(string(header))
		if len(fields) != 3 {
			return nil, fmt.Errorf("failed to read %q at %q: git answered %q", revision.Path, revision.SHA, header)
		}

		size, err := strconv.Atoi(fields[2])
		if err != nil || size+1 > len(out) {
			return nil, fmt.Errorf("failed to read %q at %q: git answered %q", revision.Path, revision.SHA, header)
		}

		if fields[1] == "blob" {
			files[revision] = out[:size:size]
		}

		out = out[size+1:]
	}

	return files, nil
}

// excludePathspecs turns paths into pathspecs git will subtract, so a caller writes only a path
// or a glob such as proto/gen.
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

// CountChangedFilesParams says which two commits to compare and what to leave out of the count.
type CountChangedFilesParams struct {
	// Dir is anywhere inside the repository holding both commits.
	Dir     string
	FromSHA string
	ToSHA   string
	Exclude []string
}

// CountChangedFiles is how many files differ between two commits.
func (c *Client) CountChangedFiles(ctx context.Context, params CountChangedFilesParams) (int, error) {
	return countChangedFiles(ctx, local, params)
}

func countChangedFiles(ctx context.Context, on runner, params CountChangedFilesParams) (int, error) {
	if params.FromSHA == "" || params.ToSHA == "" || params.FromSHA == params.ToSHA {
		return 0, nil
	}

	args := append([]string{
		"-c", "core.quotepath=false",
		"--no-pager", "diff", "--no-ext-diff", "--name-only",
		params.FromSHA, params.ToSHA, "--", ".",
	}, excludePathspecs(params.Exclude)...)

	out, err := runOn(ctx, on, params.Dir, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to compare %q against %q: %w", params.FromSHA, params.ToSHA, err)
	}
	if strings.TrimSpace(out) == "" {
		return 0, nil
	}

	return len(strings.Split(strings.TrimSpace(out), "\n")), nil
}

// DiffParams says which two commits to read the patch between and what to leave out of it.
type DiffParams struct {
	// Dir is the repository holding both commits.
	Dir string
	// FromSHA is the commit the patch is measured from, and ToSHA the one it is measured to.
	FromSHA string
	ToSHA   string
	// Exclude is the paths and globs to leave out, relative to the repository root.
	Exclude []string
}

// Diff is the patch between two commits, written with the prefixes a parser of it expects.
func (c *Client) Diff(ctx context.Context, params DiffParams) ([]byte, error) {
	return diff(ctx, local, params)
}

func diff(ctx context.Context, on runner, params DiffParams) ([]byte, error) {
	args := append([]string{
		"-c", "core.quotepath=false",
		"-c", "diff.external=",
		"-c", "diff.noprefix=false",
		"-c", "diff.mnemonicPrefix=false",
		"-c", "diff.srcPrefix=a/",
		"-c", "diff.dstPrefix=b/",
		"--no-pager", "diff",
		"--no-ext-diff", "--no-color", "--find-renames", "--unified=3", "--full-index",
		params.FromSHA, params.ToSHA, "--", ".",
	}, excludePathspecs(params.Exclude)...)

	raw, err := runBytesOn(ctx, on, params.Dir, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to diff %s against %s: %w", params.FromSHA, params.ToSHA, err)
	}

	return raw, nil
}
