package doctor

import (
	"context"
	"fmt"
	"strings"

	"komrad/internal/harness"
)

var (
	errWorkflowHarnessesUnreadable = &ProblemError{
		Code:    "workflow_harnesses_unreadable",
		Message: "the harness of each workflow could not be read",
	}
	errWorkflowHarnessUnrunnable = &ProblemError{
		Code:    "workflow_harness_unrunnable",
		Message: "a workflow's agent CLI does not run from the workflow's directory",
	}
)

const fallbackCheckShell = "/bin/sh"

// WorkflowHarness is one live workflow's directory and the harness its agents run, blank when
// they take the default.
type WorkflowHarness struct {
	Name    string
	Cwd     string
	Harness string
}

func newWorkflowHarnessCheck() Check {
	return Check{
		Name:        "workflow harnesses",
		Description: "each workflow's agent CLI has to start from the workflow's directory, where the project's own tool versions apply",
		Needs:       []string{"database", "tmux"},
		Run: func(ctx context.Context, env Env) Result {
			workflows, err := env.ReadWorkflowHarnesses(ctx, env.DatabaseURL)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", errWorkflowHarnessesUnreadable, err), "")
			}

			shell, _, err := readTmuxDefaultShell(ctx, env)
			if err != nil || shell == "" {
				shell = fallbackCheckShell
			}

			return reportWorkflowHarnesses(ctx, env, shell, workflows)
		},
	}
}

func reportWorkflowHarnesses(ctx context.Context, env Env, shell string, workflows []WorkflowHarness) Result {
	binaries := make(map[string]string, len(env.Harnesses))
	for _, agent := range env.Harnesses {
		binaries[agent.ID] = agent.Binary
	}

	var unrunnable []string
	checked := 0

	for _, workflow := range workflows {
		if strings.TrimSpace(workflow.Cwd) == "" {
			continue
		}

		id := workflow.Harness
		if id == "" {
			id = effectiveDefaultHarness(env)
		}

		binary, ok := binaries[id]
		if !ok {
			continue
		}

		script := "cd " + harness.QuoteArg(workflow.Cwd) + " && " + harness.QuoteArg(binary) + " --version"
		if _, err := env.Output(ctx, shell, "-l", "-i", "-c", script); err != nil {
			unrunnable = append(unrunnable, fmt.Sprintf("%s cannot run %s: %v", workflow.Name, binary, err))

			continue
		}

		checked++
	}

	if len(unrunnable) > 0 {
		return Result{
			Status: StatusWarn,
			Detail: strings.Join(unrunnable, "; "),
			Err:    fmt.Errorf("%w: %s", errWorkflowHarnessUnrunnable, describeCount(len(unrunnable), "workflow", "workflows")),
			Fix:    "install the CLI where the project's tool versions do not hide it, such as outside the version manager the project pins",
		}
	}

	return pass(describeCount(checked, "workflow", "workflows") + " checked")
}
