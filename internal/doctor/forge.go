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

			var lines, refused, missing []string
			for _, status := range statuses {
				switch {
				case errors.Is(status.Err, forge.ErrNoCredential):
					missing = append(missing, status.Err.Error())
				case status.Err != nil:
					refused = append(refused, fmt.Sprintf("%s: %v", status.Host, status.Err))
				default:
					lines = append(lines, status.Host+" as "+status.Login)
				}
			}

			if len(missing) > 0 {
				return warn(
					fmt.Errorf("%w: %s", ErrForgeNoCredential, strings.Join(missing, "; ")),
					strings.Join(lines, ", "),
				)
			}
			if len(refused) > 0 {
				return warn(
					fmt.Errorf("%w: %s", ErrForgeRefused, strings.Join(refused, "; ")),
					strings.Join(lines, ", "),
				)
			}

			return pass(strings.Join(lines, ", "))
		},
	}
}
