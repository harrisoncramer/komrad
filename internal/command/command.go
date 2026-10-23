package command

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// RunParams names the program to run, its arguments, and the environment it gets.
type RunParams struct {
	Path string
	Args []string
	Env  []string
}

// Run runs a program to the end and returns its output in the error when it fails.
func Run(ctx context.Context, params RunParams) error {
	cmd := exec.CommandContext(ctx, params.Path, params.Args...)
	cmd.Env = params.Env

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}
