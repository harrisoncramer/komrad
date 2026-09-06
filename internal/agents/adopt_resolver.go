package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"weave/internal/git"
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
	State           string
}

// GHPullRequests answers pullRequestResolver using the gh CLI.
type GHPullRequests struct{}

// NewGHPullRequests returns a pullRequestResolver backed by gh.
func NewGHPullRequests() *GHPullRequests {
	return &GHPullRequests{}
}

// Resolve reads a pull request named by URL or number, running in dir so that gh can work out
// the repository when the reference is a bare number.
func (GHPullRequests) Resolve(ctx context.Context, dir, ref string) (*PullRequest, error) {
	fields := "url,number,title,headRefName,baseRefName,isCrossRepository,headRepositoryOwner,state"
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
		State       string `json:"state"`
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
		State:           view.State,
	}, nil
}

// Repository reports the owner and name of the repository dir's origin points at, in the
// owner/name form a pull request URL carries.
func (GHPullRequests) Repository(ctx context.Context, dir string) (string, error) {
	url, err := git.GetOriginURL(ctx, dir)
	if err != nil {
		return "", err
	}

	return repositoryFromRemote(url), nil
}

// isPullRequestRef reports whether a reference names a pull request rather than a branch,
// which means a GitHub URL, a bare number, or a number behind a hash.
func isPullRequestRef(ref string) bool {
	if strings.Contains(ref, "/pull/") {
		return true
	}

	digits := strings.TrimPrefix(ref, "#")
	if digits == "" {
		return false
	}

	// A signed number reaches gh as a flag rather than as a pull request, so only plain digits
	// count.
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
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

// repositoryFromRemote pulls owner/name out of a git remote, which may be SSH or HTTPS, with or
// without a .git suffix.
func repositoryFromRemote(remote string) string {
	trimmed := strings.TrimSuffix(remote, ".git")
	// A remote naming a path is not a forge, so it has no owner and name to read.
	if strings.HasPrefix(trimmed, "file://") {
		return ""
	}
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
