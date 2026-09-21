package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/internal/git"
)

var (
	errWorkflowBasesUnreadable = &ProblemError{
		Code:    "workflow_bases_unreadable",
		Message: "the base branch of each workflow could not be read",
	}
	errWorkflowBaseMissing = &ProblemError{
		Code:    "workflow_base_missing",
		Message: "a workflow names a base branch its clone does not hold",
	}
)

// WorkflowBase is one live workflow's directory and the base branch it names.
type WorkflowBase struct {
	Name string
	Cwd  string
	Base string
}

// newBasesCheck reports every live workflow whose clone cannot resolve the base it names.
func newBasesCheck() Check {
	return Check{
		Name:        "bases",
		Description: "every workflow names a base branch, and its clone has to hold that branch for reviews and rebases to measure against",
		Needs:       []string{"database"},
		Run: func(ctx context.Context, env Env) Result {
			bases, err := env.ReadWorkflowBases(ctx, env.DatabaseURL)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", errWorkflowBasesUnreadable, err), "")
			}

			return reportBases(ctx, env, bases)
		},
	}
}

func reportBases(ctx context.Context, env Env, bases []WorkflowBase) Result {
	var missing []string
	checked := 0

	for _, workflow := range bases {
		if strings.TrimSpace(workflow.Cwd) == "" {
			continue
		}

		_, err := env.ResolveBase(ctx, workflow.Cwd, workflow.Base)
		if errors.Is(err, git.ErrBaseMissing) {
			missing = append(missing, fmt.Sprintf("%s names %s", workflow.Name, workflow.Base))

			continue
		}
		if err != nil {
			continue
		}

		checked++
	}

	if len(missing) > 0 {
		return Result{
			Status: StatusWarn,
			Detail: strings.Join(missing, "; "),
			Err:    fmt.Errorf("%w: %s", errWorkflowBaseMissing, describeCount(len(missing), "workflow", "workflows")),
			Fix:    "fetch the branch into the clone, or update the workflow's base branch to one the clone has",
		}
	}

	return pass(describeCount(checked, "workflow", "workflows") + " checked")
}
