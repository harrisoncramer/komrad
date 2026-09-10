package doctor

import (
	"context"
	"fmt"
)

var (
	ErrContextFileAbsent = &ProblemError{
		Code:    "context_file_absent",
		Message: "context.file names a file that is not there, so no agent is started with it",
	}
	ErrContextFileNotAFile = &ProblemError{
		Code:    "context_file_not_a_file",
		Message: "context.file names something that is not a file",
	}
	ErrContextFileUnreadable = &ProblemError{
		Code:    "context_file_unreadable",
		Message: "context.file could not be read",
	}
)

// newContextCheck asserts the standing text every agent is started with is there to read, a
// missing one being resolved at startup and logged at debug rather than said out loud.
func newContextCheck() Check {
	return Check{
		Name:        "context",
		Description: "the standing text every agent is started with, whatever workflow it is in",
		Needs:       []string{"config"},
		Run: func(_ context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrConfigUnreadable, err), "", "")
			}

			resolved, err := cfg.GetContextFile()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrHomeUnknown, err), "", "")
			}

			named := cfg.Context.File != ""
			shown := describePath(cfg.Context.File, resolved)

			state, err := env.InspectDir(resolved)
			switch {
			case err != nil:
				return fail(fmt.Errorf("%w: %w", ErrContextFileUnreadable, err), shown,
					"chmod u+r "+shown+", or point context.file at a file you can read")

			case !state.Present && named:
				return fail(fmt.Errorf("%w: %s", ErrContextFileAbsent, shown), shown,
					"write "+shown+", or drop context.file to start agents without a standing text")

			case !state.Present:
				return pass("no standing context, which is the default")

			case state.Directory:
				return fail(fmt.Errorf("%w: %s", ErrContextFileNotAFile, shown), shown,
					"point context.file at a file rather than a directory")
			}

			return pass(shown)
		},
	}
}
