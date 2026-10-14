package doctor

import (
	"context"
	"fmt"
	"strings"

	"komrad/internal/forge"
)

// connectFirstFix is how a person connects a forge, whichever they use.
const connectFirstFix = "komrad setup, or komrad forge connect <host>"

var errForgesUnreadable = &problemError{
	Code:    CodeForgesUnreadable,
	Message: "the server could not check its forges",
}

var errForgeRefused = &problemError{
	Code:    "forge_refused",
	Message: "a connected forge no longer accepts its credential",
}

var errForgeNoCredential = &problemError{
	Code:    "forge_no_credential",
	Message: "a connected forge has no credential on this machine",
}

var errNoForges = &problemError{
	Code:    CodeNoForges,
	Message: "no forge is connected, so pull requests cannot be read",
}

var errForgeUnreachable = &problemError{
	Code:    "forge_unreachable",
	Message: "a connected forge could not be reached",
}

// classifyForgeProblem says which kind of trouble the server said a connection is in.
func classifyForgeProblem(kind string) *problemError {
	switch kind {
	case "no_credential":
		return errForgeNoCredential
	case "refused":
		return errForgeRefused
	default:
		return errForgeUnreachable
	}
}

// forgeProblemRank orders the kinds of trouble, worst last.
var forgeProblemRank = map[string]int{
	errForgeUnreachable.Code:  1,
	errForgeNoCredential.Code: 2,
	errForgeRefused.Code:      3,
}

// worseForgeProblem is whichever of two problems a person has to act on first.
func worseForgeProblem(current, candidate *problemError) *problemError {
	if current == nil || forgeProblemRank[candidate.Code] > forgeProblemRank[current.Code] {
		return candidate
	}

	return current
}

// ForgeStatus is one connected forge and whether it still answers, as the server checked it.
type ForgeStatus struct {
	Host  string
	Login string
	// Kind is which forge the host runs, github or gitlab.
	Kind string
	// Method is how komrad obtained the credential, token or github_app.
	Method string
	// Problem is what went wrong, empty when the forge accepted the credential.
	Problem string
	// ProblemKind is refused, no_credential or unreachable when Problem is set.
	ProblemKind string
}

// newForgesCheck asks the server to check every forge it is connected to and warns for each
// one that no longer answers.
func newForgesCheck() Check {
	return Check{
		Name:        CheckForges,
		Description: "pull requests are read through the forges the server is connected to",
		Needs:       []string{CheckServer},
		Run: func(ctx context.Context, env Env) Result {
			statuses, err := env.CheckForges(ctx)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", errForgesUnreadable, err), "")
			}
			if len(statuses) == 0 {
				return Result{
					Status: StatusWarn,
					Err:    errNoForges,
					Fix:    connectFirstFix,
				}
			}

			var lines, failed, fixes []string
			var worst *problemError
			for _, status := range statuses {
				if status.Problem == "" {
					lines = append(lines, status.Host+" as "+status.Login)

					continue
				}

				failed = append(failed, fmt.Sprintf("%s: %s", status.Host, status.Problem))
				worst = worseForgeProblem(worst, classifyForgeProblem(status.ProblemKind))
				if fix := reconnectFix(status); fix != "" {
					fixes = append(fixes, fix)
				}
			}

			if worst != nil {
				return Result{
					Status: StatusWarn,
					Detail: strings.Join(lines, ", "),
					Err:    fmt.Errorf("%w: %s", worst, strings.Join(failed, "; ")),
					Fix:    strings.Join(fixes, "; "),
				}
			}

			return pass(strings.Join(lines, ", "))
		},
	}
}

// reconnectFix is how a person gives a connection that lost its credential a new one, empty when
// the trouble is reaching the forge rather than the credential.
func reconnectFix(status ForgeStatus) string {
	if status.ProblemKind != "refused" && status.ProblemKind != "no_credential" {
		return ""
	}

	connect := "komrad forge connect " + status.Host
	if _, err := forge.KindForHost(status.Host); err != nil {
		connect += " --kind " + status.Kind
	}

	if status.Method == forge.MethodGitHubApp {
		return connect + " --method github-app"
	}

	return connect + " --token-file <token>"
}
