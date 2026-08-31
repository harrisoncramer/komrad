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

var ErrReported = errors.New("reported")

func doctorCmd(state *app) *cobra.Command {
	var asJSON bool
	var color string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that this machine has everything weave needs",
		Long: `Check that this machine has everything weave needs, one line per dependency.

It checks tmux and its version, the shell tmux starts panes with, whether the
config file parses and names a harness that exists, the database and whether its
schema is current, the read-only login the MCP query tools connect as, the service
on its address, and the CLI binary behind each harness. It repairs nothing, since a
command that changed what it was describing could not be trusted to say what is
wrong.

The exit status is non-zero when any check failed, so it can gate a script, and
--json prints the report rather than the summary. Run this first when the panel is
doing something that makes no sense.`,
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

func (a *app) buildDoctor() (doctor.Env, []doctor.Check, error) {
	settings, err := a.buildDoctorSettings()
	if err != nil {
		return doctor.Env{}, nil, err
	}

	env := doctor.NewEnv(settings)

	env.LoadConfig = func() (*config.Config, error) { return config.Load(a.configPaths) }

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

func (a *app) buildDoctorSettings() (doctor.Settings, error) {
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

func (a *app) diagnose(ctx context.Context) []errui.Finding {
	env, checks, err := a.buildDoctor()
	if err != nil {
		return nil
	}

	report := doctor.Run(ctx, env, checks)

	failed := xslice.Filter(report.Findings, func(finding doctor.Finding) bool {
		return finding.Status == doctor.StatusFail
	})

	return xslice.Map(failed, func(finding doctor.Finding) errui.Finding {
		return errui.Finding{
			Name:   finding.Name,
			Reason: finding.Error,
			Fix:    finding.Fix,
		}
	})
}
