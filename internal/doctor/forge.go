package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/internal/forge"
)

var ErrForgesUnreadable = &ProblemError{
	Code:    "forges_unreadable",
	Message: "the connected forges could not be read",
}

var ErrForgeRefused = &ProblemError{
	Code:    "forge_refused",
	Message: "a connected forge no longer accepts its credential",
}

var ErrForgeNoCredential = &ProblemError{
	Code:    "forge_no_credential",
	Message: "a connected forge has no credential on this machine",
}

var ErrForgeUnreachable = &ProblemError{
	Code:    "forge_unreachable",
	Message: "a connected forge could not be reached",
}

// classifyForgeFailure says which kind of trouble a connection is in.
func classifyForgeFailure(err error) *ProblemError {
	switch {
	case errors.Is(err, forge.ErrNoCredential):
		return ErrForgeNoCredential
	case errors.Is(err, forge.ErrUnauthorized), errors.Is(err, forge.ErrAppReachesNothing):
		return ErrForgeRefused
	default:
		return ErrForgeUnreachable
	}
}

// forgeProblemRank orders the kinds of trouble, worst last.
var forgeProblemRank = map[string]int{
	ErrForgeUnreachable.Code:  1,
	ErrForgeNoCredential.Code: 2,
	ErrForgeRefused.Code:      3,
}

// worseForgeProblem is whichever of two problems a person has to act on first.
func worseForgeProblem(current, candidate *ProblemError) *ProblemError {
	if current == nil || forgeProblemRank[candidate.Code] > forgeProblemRank[current.Code] {
		return candidate
	}

	return current
}

// ForgeStatus is one connected forge and whether it still answers.
type ForgeStatus struct {
	Host  string
	Login string
	Err   error
}

// newForgesCheck asks every connected forge who weave is there and warns for each one that refuses.
func newForgesCheck() Check {
	return Check{
		Name:        "forges",
		Description: "pull requests are read through the forges weave is connected to",
		Needs:       []string{"config", "database"},
		Engine:      true,
		Run: func(ctx context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrConfigUnreadable, err), "", "")
			}

			statuses, err := env.VerifyForges(ctx, env.DatabaseURL, cfg.GitHubApp)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", ErrForgesUnreadable, err), "")
			}
			if len(statuses) == 0 {
				return pass("none connected")
			}

			var lines, failed []string
			var worst *ProblemError
			for _, status := range statuses {
				if status.Err == nil {
					lines = append(lines, status.Host+" as "+status.Login)

					continue
				}

				failed = append(failed, fmt.Sprintf("%s: %v", status.Host, status.Err))
				worst = worseForgeProblem(worst, classifyForgeFailure(status.Err))
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
