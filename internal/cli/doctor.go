package cli

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/appconfig"
	"weave/internal/doctor"
	"weave/internal/version"

	"github.com/spf13/cobra"
)

// ErrReported marks a command that has already written the reason to its own output. main
// prints whatever Execute hands back, so without this a report would be followed by a
// redundant sentence saying it failed. The non-zero exit is the part that matters.
var ErrReported = errors.New("reported")

// doctorCmd reports whether this machine can run weave, one line per dependency.
//
// It repairs nothing. A command that changes what it is describing cannot be run to find
// out what is wrong, and the failures it reports are the ones where the user has to decide
// what to do about them.
func doctorCmd(state *app) *cobra.Command {
	var asJSON bool
	var color string

	cmd := &cobra.Command{
		Use:           "doctor",
		Short:         "Check that this machine has everything weave needs",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			colored, err := shouldColorize(color, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			settings, err := state.doctorSettings()
			if err != nil {
				return err
			}

			env := doctor.NewEnv(settings)
			// Supplied here rather than built in doctor, so the report and the panel
			// agree about who holds the address: this is the same probe the panel
			// uses to decide whether to reuse a server or replace it.
			env.ProbeServer = func(_ context.Context, addr string) doctor.ServerStatus {
				found := probeServer(addr)

				return doctor.ServerStatus{
					Answering: found.answering,
					Ready:     found.ready,
					Version:   found.version,
					Current:   found.current,
				}
			}

			report := doctor.Run(cmd.Context(), env, doctor.Checks(settings))

			if asJSON {
				err = doctor.WriteJSON(cmd.OutOrStdout(), report)
			} else {
				err = doctor.WriteText(cmd.OutOrStdout(), report, colored)
			}
			if err != nil {
				return err
			}

			// A failing machine exits non-zero so this scripts, and the report has
			// already said what is wrong, so main must not print an error over it.
			if !report.Summary.Healthy {
				return ErrReported
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw report instead of the summary")
	cmd.Flags().StringVar(&color, "color", "auto", "auto, always, or never")

	return cmd
}

// doctorSettings is what this build would run with, gathered here so no check reads a file
// or an environment for itself.
//
// The harnesses come from the registry rather than a list written twice, so a build that
// gains a third agent CLI gains a line in the report without this being touched.
func (a *app) doctorSettings() (doctor.Settings, error) {
	configPath, err := appconfig.ResolvePath(a.configPath)
	if err != nil {
		return doctor.Settings{}, fmt.Errorf("failed to work out where the config lives: %w", err)
	}

	harnesses := make([]doctor.HarnessInfo, 0)
	for _, id := range a.harnesses.IDs() {
		found, err := a.harnesses.GetByID(id)
		if err != nil {
			return doctor.Settings{}, err
		}

		harnesses = append(harnesses, doctor.HarnessInfo{ID: found.GetID(), Binary: found.GetBinary()})
	}

	return doctor.Settings{
		TmuxSocket:          a.env.TmuxSocket,
		InsideTmux:          a.env.Tmux,
		ConfigPath:          configPath,
		Harnesses:           harnesses,
		DatabaseURL:         a.env.DatabaseURL,
		ReadOnlyDatabaseURL: a.env.ReadOnlyDatabaseURL,
		Addr:                a.env.Addr,
		Version:             version.Version(),
	}, nil
}
