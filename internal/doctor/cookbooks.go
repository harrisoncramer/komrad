package doctor

import (
	"context"
	"fmt"
	"strings"

	"weave/internal/cookbooks"
)

// ErrCookbookInvalid reports that a cookbook in the user's own directory would be refused by
// install: a manifest that will not parse, a setup or cleanup action naming no sibling file,
// or a sibling file that is not a script. It says nothing about the built-ins, which ship with
// this binary and are not the user's to break.
var ErrCookbookInvalid = &Problem{
	Code:    "cookbook_invalid",
	Message: "a cookbook would be refused by weave cookbook install",
}

// Cookbooks asserts every cookbook in the user's own directory is one install would accept.
func Cookbooks() Check {
	return Check{
		Name:        "cookbooks",
		Description: "weave cookbook install reads a manifest and the actions beside it from here",
		Needs:       []string{"config"},
		Run: func(_ context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrConfigUnreadable, err), "", "")
			}

			dir, err := cfg.GetCookbooksDir()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrCookbookInvalid, err), "", "")
			}

			found, err := cookbooks.Discover(dir)
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrCookbookInvalid, err), dir, "")
			}
			if len(found) == 0 {
				return pass("no cookbooks in " + dir)
			}

			var broken []string
			for _, cookbook := range found {
				if err := cookbook.Validate(); err != nil {
					broken = append(broken, fmt.Sprintf("%s (%s)", cookbook.Name, err))
				}
			}
			if len(broken) == 0 {
				return pass(fmt.Sprintf("%d cookbook(s) in %s", len(found), dir))
			}

			return fail(
				fmt.Errorf("%w: %s", ErrCookbookInvalid, strings.Join(broken, "; ")),
				dir,
				"fix or remove the cookbook named above",
			)
		},
	}
}
