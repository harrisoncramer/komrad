package git

import (
	"context"
	"fmt"
	"strings"
)

// GetRoot is the top of the working tree any path inside it belongs to.
func GetRoot(ctx context.Context, dir string) (string, error) {
	root, err := Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("failed to find the repository at %q: %w", dir, err)
	}

	return root, nil
}

// resolveBase turns a configured base branch into something git can name, preferring a
// remote-tracking ref since a worktree is cut from one.
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

// GetCurrentBranch is the branch a worktree is on, and nothing for a detached head or a directory
// git will not answer about, since neither is a branch anybody can be told about.
func GetCurrentBranch(ctx context.Context, dir string) *string {
	branch, err := Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" || branch == "HEAD" {
		return nil
	}

	return &branch
}

// GetOriginURL is where a checkout pushes to, which is what says which repository it belongs to.
func GetOriginURL(ctx context.Context, dir string) (string, error) {
	url, err := Run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("failed to read the origin of %q: %w", dir, err)
	}

	return url, nil
}

// ReadFile is one file as a commit holds it.
func ReadFile(ctx context.Context, dir, sha, path string) ([]byte, error) {
	out, err := RunBytes(ctx, dir, "show", sha+":"+path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q at %q: %w", path, sha, err)
	}

	return out, nil
}

// ReadFileLines is one file as a commit holds it, split into lines.
func ReadFileLines(ctx context.Context, dir, sha, path string) ([]string, error) {
	out, err := ReadFile(ctx, dir, sha, path)
	if err != nil {
		return nil, err
	}

	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"), nil
}

// ExcludePathspecs turns paths into pathspecs git will subtract, adding the exclusion magic so a
// caller writes only a path or a glob such as proto/gen.
func ExcludePathspecs(paths []string) []string {
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

// CountChangedFiles is how many files differ between two commits, which is what says how much of a
// snapshot is new since somebody last looked at one.
func CountChangedFiles(ctx context.Context, params CountChangedFilesParams) (int, error) {
	if params.FromSHA == "" || params.ToSHA == "" || params.FromSHA == params.ToSHA {
		return 0, nil
	}

	args := append([]string{
		"-c", "core.quotepath=false",
		"--no-pager", "diff", "--no-ext-diff", "--name-only",
		params.FromSHA, params.ToSHA, "--", ".",
	}, ExcludePathspecs(params.Exclude)...)

	out, err := Run(ctx, params.Dir, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to compare %q against %q: %w", params.FromSHA, params.ToSHA, err)
	}
	if strings.TrimSpace(out) == "" {
		return 0, nil
	}

	return len(strings.Split(strings.TrimSpace(out), "\n")), nil
}
