package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/doctor"
	"weave/internal/errui"
	"weave/internal/version"
	"weave/lib/xslice"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ErrReported marks a command that has already written the reason to its own output, so main
// (which prints whatever Execute hands back) does not follow the report with a redundant
// failure line; the non-zero exit is the part that matters.
var ErrReported = errors.New("reported")

// doctorCmd reports whether this machine can run weave, one line per dependency; it repairs
// nothing, since a command that changes what it is describing cannot be trusted to say what
// is wrong.
func doctorCmd(state *app) *cobra.Command {
	var asJSON bool
	var color string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that this machine has everything weave needs",
		Long: "Check that this machine has everything weave needs, one line per dependency.\n\n" +
			"It checks tmux and its version, the shell tmux starts panes with, whether the\n" +
			"config file parses and names a harness that exists, whether your actions and\n" +
			"cookbooks would run as written, the database and whether its schema is current,\n" +
			"the read-only login the MCP query tools connect as, the service on its address, and\n" +
			"the CLI binary behind each harness. It repairs nothing, since a command that\n" +
			"changed what it was describing could not be trusted to say what is wrong.\n\n" +
			"The exit status is non-zero when any check failed, so it can gate a script, and\n" +
			"--json prints the report rather than the summary. Run this first when the panel is\n" +
			"doing something that makes no sense.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			colored, err := shouldColorize(color, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			env, checks, err := state.buildDoctor()
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

			// A failing machine exits non-zero so this can gate a script; the report already
			// said what is wrong, so main must not print an error over it.
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

// buildDoctor is the checks and what they run against; two callers need it, this command and
// the panel explaining why its server would not start, and building it twice is what leaves
// the seam nil for whichever caller doesn't need it.
func (a *app) buildDoctor() (doctor.Env, []doctor.Check, error) {
	settings, err := a.buildDoctorSettings()
	if err != nil {
		return doctor.Env{}, nil, err
	}

	env := doctor.NewEnv(settings)

	// The paths as given rather than as resolved, since only the unresolved list can tell a
	// config nobody wrote apart from one somebody named that is not there.
	env.LoadConfig = func() (*config.Config, error) { return config.Load(a.configPaths) }

	// The same probe the panel uses to decide whether to reuse a server or replace it, so
	// the two cannot disagree about who holds the address.
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

// buildDoctorSettings is what this build would run with, gathered here so no check reads a
// file or environment for itself; harnesses come from the registry, so a build that gains a
// third agent CLI gains a report line without this being touched.
func (a *app) buildDoctorSettings() (doctor.Settings, error) {
	// A layer list that will not resolve is left for LoadConfig to raise, rather than
	// aborting here, since doctor is in commandsSurvivingBadConfig and must reach the report
	// rather than abort before any check runs.
	layers, err := config.ResolveLayers(a.configPaths)
	if err != nil {
		layers = nil
	}

	harnesses := make([]doctor.HarnessInfo, 0, len(a.harnesses.IDs()))
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
		PanelSession:        agents.ManagerSession,
		ConfigLayers:        layers,
		Sources:             a.configSources,
		Harnesses:           harnesses,
		DatabaseURL:         a.env.DatabaseURL,
		ReadOnlyDatabaseURL: a.env.ReadOnlyDatabaseURL,
		Addr:                a.env.Addr,
		Version:             version.Version(),
	}, nil
}

// shouldColorize decides whether to write escapes.
func shouldColorize(mode string, out io.Writer) (bool, error) {
	switch mode {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto":
		file, ok := out.(*os.File)

		return ok && term.IsTerminal(int(file.Fd())), nil
	default:
		return false, fmt.Errorf("unknown color %q, expected auto, always, or never", mode)
	}
}

// diagnose is the doctor's failing findings, for a caller that otherwise has only a log
// tail to explain itself with.
func (a *app) diagnose(ctx context.Context) []errui.Finding {
	env, checks, err := a.buildDoctor()
	if err != nil {
		return nil
	}

	report := doctor.Run(ctx, env, checks)

	// Failures only: a warning was true before the server failed to start and will be true
	// after it works.
	failed := xslice.Filter(report.Findings, func(finding doctor.Finding) bool {
		return finding.Status == doctor.StatusFail
	})

	return xslice.Map(failed, func(finding doctor.Finding) errui.Finding {
		return errui.Finding{Name: finding.Name, Reason: finding.Error, Fix: finding.Fix}
	})
}
