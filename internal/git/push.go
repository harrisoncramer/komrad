package git

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"komrad/internal/fail"
)

// pushTimeout is how long a push is given, which a pre-push hook running a test suite needs.
const pushTimeout = 10 * time.Minute

// ErrPushRejected says origin holds commits the branch never took in.
var ErrPushRejected = fail.Precondition("origin has commits this branch never took in, so the agent has to bring them in before it can be pushed")

// pushRejections are the reasons git gives for refusing a push that bringing origin's commits in
// would settle.
var pushRejections = []string{"stale info", "remote ref updated since checkout", "fetch first", "non-fast-forward"}

// PushParams names the checkout, the branch sent to origin, and whether origin's copy may be
// overwritten.
type PushParams struct {
	Dir    string
	Branch string
	Force  bool
}

// Push sends the branch to origin under its own name and answers the commit origin now holds,
// overwriting origin's copy only when Force says so and the branch already holds what is there.
func (c *Client) Push(ctx context.Context, params PushParams) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()

	ref := "refs/heads/" + params.Branch
	args := []string{"-c", "credential.interactive=never", "push", "--porcelain"}
	if params.Force {
		args = append(args, "--force-with-lease", "--force-if-includes")
	}

	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := runReporting(ctx, params.Dir, env, append(args, "origin", ref+":"+ref)...)
	if isPushRejected(out) {
		return "", fmt.Errorf("%w: %s", ErrPushRejected, params.Branch)
	}
	if err != nil {
		return "", err
	}

	sha, err := Run(ctx, params.Dir, "rev-parse", "--verify", "refs/remotes/origin/"+params.Branch)
	if err != nil {
		return "", fmt.Errorf("failed to read what origin holds of %s after the push: %w", params.Branch, err)
	}

	return sha, nil
}

// isPushRejected reads git push --porcelain for a ref turned away because origin moved on.
func isPushRejected(porcelain string) bool {
	for line := range strings.Lines(porcelain) {
		if !strings.HasPrefix(line, "!") {
			continue
		}

		for _, reason := range pushRejections {
			if strings.Contains(line, "("+reason+")") {
				return true
			}
		}
	}

	return false
}
