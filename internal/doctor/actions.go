package doctor

import (
	"context"
	"fmt"
	"strings"

	"weave/internal/actions"
)

// ErrActionInvalid reports that a file in the user's own actions directory would not run as
// written: not executable, text with no shebang, or a header directive weave could not read.
var ErrActionInvalid = &Problem{
	Code:    "action_invalid",
	Message: "an action would not run as written",
}

// Actions asserts every file in the user's own actions directory would run if fired. It says
// nothing about the built-ins, which ship with this binary and are rewritten on every start.
func Actions() Check {
	return Check{
		Name:        "actions",
		Description: "the panel's action key and weave cookbook install run scripts from here",
		Needs:       []string{"config"},
		Run: func(_ context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrConfigUnreadable, err), "", "")
			}

			dir, err := cfg.GetActionsDir()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrActionInvalid, err), "", "")
			}

			found, err := actions.Discover(dir)
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrActionInvalid, err), dir, "")
			}
			if len(found) == 0 {
				return pass("no actions in " + dir)
			}

			var broken []string
			for _, action := range found {
				if err := action.Validate(); err != nil {
					broken = append(broken, fmt.Sprintf("%s (%s)", action.Name, err))
				}
			}
			if len(broken) == 0 {
				return pass(fmt.Sprintf("%d action(s) in %s", len(found), dir))
			}

			return fail(
				fmt.Errorf("%w: %s", ErrActionInvalid, strings.Join(broken, "; ")),
				dir,
				"fix or remove the action named above",
			)
		},
	}
}
