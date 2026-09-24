package doctor

import (
	"context"
	"fmt"
	"strings"
)

var errForgesUnreadable = &ProblemError{
	Code:    "forges_unreadable",
	Message: "the service could not check its forges",
}

var errForgeRefused = &ProblemError{
	Code:    "forge_refused",
	Message: "a connected forge no longer accepts its credential",
}

var errForgeNoCredential = &ProblemError{
	Code:    "forge_no_credential",
	Message: "a connected forge has no credential on this machine",
}

var errNoForges = &ProblemError{
	Code:    "no_forges",
	Message: "no forge is connected, so pull requests cannot be read",
}

var errForgeUnreachable = &ProblemError{
	Code:    "forge_unreachable",
	Message: "a connected forge could not be reached",
}

// classifyForgeProblem says which kind of trouble the service said a connection is in.
func classifyForgeProblem(kind string) *ProblemError {
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
func worseForgeProblem(current, candidate *ProblemError) *ProblemError {
	if current == nil || forgeProblemRank[candidate.Code] > forgeProblemRank[current.Code] {
		return candidate
	}

	return current
}

// ForgeStatus is one connected forge and whether it still answers, as the service checked it.
type ForgeStatus struct {
	Host  string
	Login string
	// Problem is what went wrong, empty when the forge accepted the credential.
	Problem string
	// ProblemKind is refused, no_credential or unreachable when Problem is set.
	ProblemKind string
}

// newForgesCheck asks the service to check every forge it is connected to and warns for each
// one that no longer answers.
func newForgesCheck() Check {
	return Check{
		Name:        "forges",
		Description: "pull requests are read through the forges the service is connected to",
		Needs:       []string{"server"},
		Run: func(ctx context.Context, env Env) Result {
			statuses, err := env.CheckForges(ctx)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", errForgesUnreadable, err), "")
			}
			if len(statuses) == 0 {
				return warn(errNoForges, "gh auth token | komrade forge connect github.com")
			}

			var lines, failed []string
			var worst *ProblemError
			for _, status := range statuses {
				if status.Problem == "" {
					lines = append(lines, status.Host+" as "+status.Login)

					continue
				}

				failed = append(failed, fmt.Sprintf("%s: %s", status.Host, status.Problem))
				worst = worseForgeProblem(worst, classifyForgeProblem(status.ProblemKind))
			}

			if worst != nil {
				return warn(
					fmt.Errorf("%w: %s", worst, strings.Join(failed, "; ")),
					strings.Join(lines, ", "),
				)
			}

			return pass(strings.Join(lines, ", "))
		},
	}
}
