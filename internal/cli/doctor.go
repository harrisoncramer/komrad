package cli

import (
	"errors"

	"weave/internal/doctor"

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

			env := doctor.NewEnv(doctor.Settings{
				TmuxSocket: state.env.TmuxSocket,
				InsideTmux: state.env.Tmux,
			})

			report := doctor.Run(cmd.Context(), env, doctor.Checks())

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
