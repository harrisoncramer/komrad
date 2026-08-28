package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// GHPRFinder answers prFinder using the gh CLI, which infers the repository from the
// remote of the directory it runs in.
type GHPRFinder struct{}

// NewGHPRFinder returns a prFinder backed by gh.
func NewGHPRFinder() *GHPRFinder {
	return &GHPRFinder{}
}

// FindPR returns the URL of a pull request for branch, or empty when gh reports none.
func (GHPRFinder) FindPR(ctx context.Context, dir, branch string) (string, error) {
	// This runs once per agent as it goes idle, so a pull request closed or merged in the
	// meantime still counts as found.
	cmd := exec.CommandContext(ctx, "gh", "pr", "list", "--head", branch, "--state", "all", "--json", "url", "--limit", "1")
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh pr list --head %s: %w%s", branch, err, ghStderrDetail(err))
	}

	var prs []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return "", fmt.Errorf("failed to parse gh's pull request list: %w", err)
	}
	if len(prs) == 0 {
		return "", nil
	}

	return prs[0].URL, nil
}

func ghStderrDetail(err error) string {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return ""
	}

	detail := strings.TrimSpace(string(exitErr.Stderr))
	if detail == "" {
		return ""
	}

	return ": " + detail
}

// isGHRateLimitError reports whether a failed gh call was GitHub turning it away for rate
// limiting rather than some other failure.
func isGHRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	// gh words both the primary and secondary limit differently across its subcommands and
	// across API versions, so this matches loosely on purpose.
	return strings.Contains(msg, "rate limit") || strings.Contains(msg, "abuse detection")
}
