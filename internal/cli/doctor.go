package cli

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/appconfig"
	"weave/internal/doctor"
	"weave/internal/errui"
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

			env, checks, err := state.doctor()
			if err != nil {
				return err
			}

			report := doctor.Run(cmd.Context(), env, checks)

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

// doctor is the checks and what they run against, built in one place because there are two
// callers: the command, and the panel explaining why its server would not start. Building
// it twice is how a seam gets filled in for one of them and left nil for the other.
func (a *app) doctor() (doctor.Env, []doctor.Check, error) {
	settings, err := a.doctorSettings()
	if err != nil {
		return doctor.Env{}, nil, err
	}

	env := doctor.NewEnv(settings)

	// Supplied here rather than built inside doctor, so the report and the panel agree
	// about who holds the address: this is the same probe the panel uses to decide
	// whether to reuse a server or replace it.
	env.ProbeServer = func(_ context.Context, addr string) doctor.ServerStatus {
		found := probeServer(addr)

		return doctor.ServerStatus{
			Answering: found.answering,
			Ready:     found.ready,
			Version:   found.version,
			Current:   found.current,
		}
	}

	return env, doctor.Checks(settings), nil
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

// diagnose is the doctor's failing findings, for a caller that has to explain a failure it
// only has a log tail for. It returns nothing when the checks cannot even be assembled,
// since a screen with no advice beats one carrying an error about why there is no advice.
func (a *app) diagnose(ctx context.Context) []errui.Finding {
	env, checks, err := a.doctor()
	if err != nil {
		return nil
	}

	report := doctor.Run(ctx, env, checks)

	var found []errui.Finding
	for _, finding := range report.Findings {
		// Failures only. A warning is a fact that was true before the server failed
		// to start and will be true after it works.
		if finding.Status != doctor.StatusFail {
			continue
		}

		found = append(found, errui.Finding{
			Name:   finding.Name,
			Reason: finding.Error,
			Fix:    finding.Fix,
		})
	}

	return found
}
