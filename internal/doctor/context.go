package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"
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

// Explainer: this reads the file rather than asking whether it is there, because the launcher
// reads it too and drops the error, so a file weave cannot read leaves every agent without its
// standing text. Presence would pass a file at mode 000, which is the same silence this row was
// added to break.

// newContextCheck asserts the standing text every agent is started with is there to read.
func newContextCheck() Check {
	return Check{
		Name:        "context",
		Description: "the standing text every agent is started with, whatever workflow it is in",
		Needs:       []string{"config"},
		Engine:      true,
		Run: func(_ context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrConfigUnreadable, err), "", "")
			}

			resolved, err := cfg.GetContextFile()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrHomeUnknown, err), "", "")
			}

			named := strings.TrimSpace(cfg.Context.File) != ""
			shown := describePath(cfg.Context.File, resolved)

			text, err := env.ReadFile(resolved)
			switch {
			case errors.Is(err, fs.ErrNotExist) && named:
				return fail(fmt.Errorf("%w: %s", ErrContextFileAbsent, shown), shown,
					"write "+shown+", or drop context.file to start agents without a standing text")

			case errors.Is(err, fs.ErrNotExist):
				return pass("no standing context, which is the default")

			case isDirectoryError(err):
				return fail(fmt.Errorf("%w: %s", ErrContextFileNotAFile, shown), shown,
					"point context.file at a file rather than a directory")

			case err != nil:
				return fail(fmt.Errorf("%w: %w", ErrContextFileUnreadable, err), shown,
					"chmod u+r "+shown+", or point context.file at a file you can read")
			}

			return pass(shown + ", " + describeCount(len(strings.Fields(string(text))), "word", "words"))
		},
	}
}

// isDirectoryError reports whether a read failed because the path is a directory.
func isDirectoryError(err error) bool {
	return errors.Is(err, syscall.EISDIR)
}
