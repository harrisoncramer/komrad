package git

import (
	"context"
	"fmt"
	"strings"
)

// GetRoot is the top of the working tree any path inside it belongs to.
func (c *Client) GetRoot(ctx context.Context, dir string) (string, error) {
	root, err := Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("failed to find the repository at %q: %w", dir, err)
	}

	return root, nil
}

// resolveBase turns a configured base branch into something git can name, preferring a
// remote-tracking ref.
func resolveBase(ctx context.Context, root, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		if head, err := Run(ctx, root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
			return head
		}

		return "HEAD"
	}

	if _, err := Run(ctx, root, "rev-parse", "--verify", "--quiet", "origin/"+base); err == nil {
		return "origin/" + base
	}

	return base
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
	if params.FromSHA == "" || params.ToSHA == "" || params.FromSHA == params.ToSHA {
		return 0, nil
	}

	args := append([]string{
		"-c", "core.quotepath=false",
		"--no-pager", "diff", "--no-ext-diff", "--name-only",
		params.FromSHA, params.ToSHA, "--", ".",
	}, excludePathspecs(params.Exclude)...)

	out, err := Run(ctx, params.Dir, args...)
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

	raw, err := RunBytes(ctx, params.Dir, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to diff %s against %s: %w", params.FromSHA, params.ToSHA, err)
	}

	return raw, nil
}
