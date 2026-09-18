package doctor

import (
	"context"
	"fmt"
	"strings"
)

var ErrForgesUnreadable = &ProblemError{
	Code:    "forges_unreadable",
	Message: "the connected forges could not be read",
}

var ErrForgeRefused = &ProblemError{
	Code:    "forge_refused",
	Message: "a connected forge no longer accepts its credential",
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
		Needs:       []string{"database"},
		Engine:      true,
		Run: func(ctx context.Context, env Env) Result {
			statuses, err := env.VerifyForges(ctx, env.DatabaseURL)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", ErrForgesUnreadable, err), "")
			}
			if len(statuses) == 0 {
				return pass("none connected")
			}

			var lines, refused []string
			for _, status := range statuses {
				if status.Err != nil {
					refused = append(refused, fmt.Sprintf("%s: %v", status.Host, status.Err))

					continue
				}

				lines = append(lines, status.Host+" as "+status.Login)
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
