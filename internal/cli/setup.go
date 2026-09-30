package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"komrad/config"
	"komrad/internal/doctor"
	"komrad/internal/setup"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func setupCmd(state *app) *cobra.Command {
	var noInput bool
	var harnessFlag string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set this machine up for komrad, asking for whatever is missing",
		Long: `Set this machine up for komrad, asking for whatever is missing.

Each step asks the doctor whether its part of the machine is ready, and only when it is not
does it ask a question and change something, then asks the doctor again. Nothing records
which steps ran, so running it again after stopping part way finds the finished steps
already done. It ends on the doctor's full report.

With --no-input, or when stdin is not a terminal, nothing is asked: a step that needs an
answer takes it from a flag, and names the flag when none was given.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			steps, err := state.setupSteps(harnessFlag)
			if err != nil {
				return err
			}

			check := func(ctx context.Context, names ...string) doctor.Report {
				env, settings, err := state.buildDoctor()
				if err != nil {
					return doctor.Report{Findings: []doctor.Finding{{
						Name:   "checks",
						Status: doctor.StatusFail,
						Error:  err.Error(),
					}}}
				}

				return doctor.Run(ctx, env, doctor.Select(doctor.Checks(settings), names...))
			}

			interactive := !noInput && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
			if interactive {
				err = setup.Run(ctx, check, steps)
			} else {
				err = setup.RunUnattended(ctx, cmd.OutOrStdout(), check, steps)
			}
			if errors.Is(err, setup.ErrAborted) {
				return err
			}

			if _, writeErr := fmt.Fprintln(cmd.OutOrStdout()); writeErr != nil {
				return writeErr
			}

			report, reportErr := state.runDoctor(ctx, interactive)
			if reportErr != nil {
				return reportErr
			}
			if writeErr := doctor.WriteText(cmd.OutOrStdout(), report, interactive); writeErr != nil {
				return writeErr
			}

			if err != nil {
				return ErrReported
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&noInput, "no-input", false, "ask nothing, taking every answer from a flag")
	cmd.Flags().StringVar(&harnessFlag, "harness", "", "the harness new agents run, when the default one is not on PATH")

	return cmd
}

func (a *app) setupSteps(harnessFlag string) ([]setup.Step, error) {
	layers, err := config.ResolveLayers(config.Stack{Paths: a.configStack.Paths})
	if err != nil {
		return nil, err
	}
	path := layers[len(layers)-1].Path

	return []setup.Step{
		&setup.ConfigStep{Path: path},
		&setup.TmuxStep{},
		&setup.HarnessStep{
			Path:      path,
			Stack:     a.configStack,
			Harnesses: a.harnesses.IDs(),
			Flag:      harnessFlag,
		},
		&setup.ContextStep{Stack: a.configStack},
		&setup.AutoSetupStep{Path: path, Stack: a.configStack},
	}, nil
}
