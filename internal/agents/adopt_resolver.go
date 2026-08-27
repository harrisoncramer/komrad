package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// PullRequest is what weave needs to know about a pull request somebody else opened.
type PullRequest struct {
	URL             string
	Number          int
	Title           string
	Branch          string
	BaseBranch      string
	CrossRepository bool
	Repository      string
}

// GHPullRequests answers pullRequestResolver using the gh CLI.
type GHPullRequests struct{}

// NewGHPullRequests returns a pullRequestResolver backed by gh.
func NewGHPullRequests() *GHPullRequests {
	return &GHPullRequests{}
}

// Resolve reads a pull request named by URL or number. It runs in dir, which is how gh
// works out the repository when the reference is a bare number, and a reference that names
// a repository of its own is checked against dir's rather than trusted: gh answers happily
// about a repository the workflow knows nothing about, and the worktree it would then be
// asked to make has no such branch.
func (GHPullRequests) Resolve(ctx context.Context, dir, ref string) (*PullRequest, error) {
	fields := "url,number,title,headRefName,baseRefName,isCrossRepository,headRepositoryOwner"
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", ref, "--json", fields)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view %s: %w%s", ref, err, ghStderrDetail(err))
	}

	var view struct {
		URL         string `json:"url"`
		Number      int    `json:"number"`
		Title       string `json:"title"`
		HeadRefName string `json:"headRefName"`
		BaseRefName string `json:"baseRefName"`
		Cross       bool   `json:"isCrossRepository"`
	}
	if err := json.Unmarshal(out, &view); err != nil {
		return nil, fmt.Errorf("failed to parse gh's pull request: %w", err)
	}
	if view.HeadRefName == "" {
		return nil, fmt.Errorf("gh answered about %s without naming a branch", ref)
	}

	return &PullRequest{
		URL:             view.URL,
		Number:          view.Number,
		Title:           view.Title,
		Branch:          view.HeadRefName,
		BaseBranch:      view.BaseRefName,
		CrossRepository: view.Cross,
		Repository:      repositoryFromURL(view.URL),
	}, nil
}

// Repository reports the owner and name of the repository dir's origin points at, in the
// owner/name form a pull request URL carries.
func (GHPullRequests) Repository(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("git -C %s remote get-url origin: %w", dir, err)
	}

	return repositoryFromRemote(strings.TrimSpace(string(out))), nil
}

// looksLikePullRequest reports whether a reference names a pull request rather than a
// branch. A branch may be called anything, so the two are told apart by the shapes a pull
// request comes in: a GitHub URL, a bare number, or a number behind a hash.
func looksLikePullRequest(ref string) bool {
	if strings.Contains(ref, "/pull/") {
		return true
	}

	_, err := strconv.Atoi(strings.TrimPrefix(ref, "#"))

	return err == nil
}

// repositoryFromURL pulls owner/name out of a pull request URL.
func repositoryFromURL(url string) string {
	parts := strings.Split(strings.Trim(url, "/"), "/")
	for i, part := range parts {
		if part == "pull" && i >= 2 {
			return parts[i-2] + "/" + parts[i-1]
		}
	}

	return ""
}

// repositoryFromRemote pulls owner/name out of a git remote, which may be an SSH remote, an
// HTTPS one, or either with a .git suffix. A remote naming a path rather than a host is not
// a forge and has no owner and name to read, so it comes back blank rather than as the last
// two segments of somebody's home directory.
func repositoryFromRemote(remote string) string {
	trimmed := strings.TrimSuffix(remote, ".git")
	if !strings.Contains(trimmed, "://") && !strings.Contains(trimmed, "@") && !strings.Contains(trimmed, ":") {
		return ""
	}

	if _, rest, found := strings.Cut(trimmed, "@"); found {
		trimmed = rest
	}
	if _, rest, found := strings.Cut(trimmed, "://"); found {
		trimmed = rest
	}

	trimmed = strings.ReplaceAll(trimmed, ":", "/")

	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 3 {
		return ""
	}

	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}
