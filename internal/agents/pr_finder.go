package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// GHPRFinder answers prFinder using the gh CLI, which infers the repository from the
// remote of the directory it runs in.
type GHPRFinder struct{}

// NewGHPRFinder returns a prFinder backed by gh.
func NewGHPRFinder() *GHPRFinder {
	return &GHPRFinder{}
}

// FindPR returns the URL of a pull request for branch, or empty when gh reports none.
// This only ever runs once per agent, right as it goes idle, so it asks for a pull
// request in any state: one closed or merged in the moment between the agent opening it
// and this check running still counts as found.
func (GHPRFinder) FindPR(ctx context.Context, dir, branch string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "list", "--head", branch, "--state", "all", "--json", "url", "--limit", "1")
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh pr list --head %s: %w", branch, err)
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
