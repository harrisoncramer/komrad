package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"weave/config"
	"weave/internal/doctor"
	"weave/internal/errui"
	"weave/internal/ui"
	"weave/internal/version"
	"weave/lib/xhttp"
	"weave/lib/xslice"
	doctorv1 "weave/proto/gen/doctor/v1"
	"weave/proto/gen/doctor/v1/doctorv1connect"
	forgev1 "weave/proto/gen/forge/v1"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/protobuf/types/known/emptypb"
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
config file parses and names a harness that exists, the hooks and worktrees
directories and whether any worktree there is left without an agent, the database
and whether its schema is current, the read-only login the MCP query tools connect
as, the service on its address, and the CLI binary behind each harness. It repairs
nothing, since a command that changed what it was describing could not be trusted
to say what is wrong.

It also asks the terminal what it can draw. Every glyph the agents list uses is
written out and the cursor position queried, so what comes back is how far the
cursor moved rather than how wide the ink was. Inside tmux that answer comes
from tmux's own grid rather than from the terminal underneath it. Bold and
italic are read out of the terminfo entry of the terminal tmux paints to, and
colour depth out of the entry this process emits for, which is what an entry
claims rather than what a terminal does. Nothing is asked of a terminal that is
not there, and whether the font has a glyph at all is not tested.

Pointed at an engine with --engine, the checks about this machine run here and the
ones about the engine are asked of its service, which makes them on its own
machine; they are printed under an "on the engine" heading, and marked not checked
when the service cannot be reached or is running a different build.

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

			report, err := state.runDoctor(cmd.Context(), true)
			if err != nil {
				return err
			}

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

// runDoctor makes the checks about this machine and, when the engine is elsewhere, asks its
// service for the checks about that one; onTTY is whether a person typed the command.
func (a *app) runDoctor(ctx context.Context, onTTY bool) (doctor.Report, error) {
	env, settings, err := a.buildDoctor()
	if err != nil {
		return doctor.Report{}, err
	}

	// The terminal is taken over only for the command a person typed.
	if onTTY {
		env.MeasureColumns = doctor.MeasureColumnsOnTTY
	}

	if !a.engine().IsRemote() {
		return doctor.Run(ctx, env, doctor.Checks(settings)), nil
	}

	report := doctor.Run(ctx, env, doctor.HereChecks())
	if !isServerOK(report) {
		return report.WithEngine([]doctor.Finding{doctor.EngineNotChecked()}), nil
	}

	findings, err := a.askEngineDoctor(ctx)
	if err != nil {
		return report.WithEngine([]doctor.Finding{doctor.EngineUnanswered(err)}), nil
	}

	return report.WithEngine(findings), nil
}

func isServerOK(report doctor.Report) bool {
	for _, finding := range report.Findings {
		if finding.Name == "server" {
			return finding.Status == doctor.StatusOK
		}
	}

	return false
}

// askEngineDoctor is the checks the engine's service made on its own machine, read back into
// the doctor's own findings.
func (a *app) askEngineDoctor(ctx context.Context) ([]doctor.Finding, error) {
	addr, release, err := a.dialAPI(ctx)
	if err != nil {
		return nil, err
	}

	defer release()

	client := doctorv1connect.NewDoctorServiceClient(xhttp.NewClient(apiCallTimeout), "http://"+addr)
	resp, err := client.RunDoctor(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		return nil, err
	}

	findings := xslice.Map(resp.Msg.GetFindings(), func(finding *doctorv1.Finding) doctor.Finding {
		return doctor.Finding{
			Name:        finding.GetName(),
			Description: finding.GetDescription(),
			Status:      doctor.Status(finding.GetStatus()),
			Detail:      finding.GetDetail(),
			Code:        finding.GetCode(),
			Error:       finding.GetError(),
			Fix:         finding.GetFix(),
		}
	})

	return findings, nil
}

func (a *app) buildDoctor() (doctor.Env, doctor.Settings, error) {
	settings, err := a.buildDoctorSettings()
	if err != nil {
		return doctor.Env{}, doctor.Settings{}, err
	}

	env := doctor.NewEnv(settings)

	// A remote engine answers on its own loopback, so the address it was configured with is
	// reached through a forward rather than dialled here.
	env.ProbeServer = func(ctx context.Context) (doctor.ServerStatus, error) {
		addr, release, err := a.dialAPI(ctx)
		if err != nil {
			return doctor.ServerStatus{}, err
		}

		defer release()

		found := probeServer(ctx, addr)

		return doctor.ServerStatus{
			Answering: found.answering,
			Ready:     found.ready,
			Version:   found.version,
			Commit:    found.commit,
			Current:   found.current,
		}, nil
	}

	env.CheckForges = func(ctx context.Context) ([]doctor.ForgeStatus, error) {
		addr, release, err := a.dialAPI(ctx)
		if err != nil {
			return nil, err
		}

		defer release()

		resp, err := forgeClient("http://"+addr).CheckForges(ctx, connect.NewRequest(&emptypb.Empty{}))
		if err != nil {
			return nil, err
		}

		statuses := xslice.Map(resp.Msg.GetChecks(), func(check *forgev1.ForgeCheck) doctor.ForgeStatus {
			return doctor.ForgeStatus{
				Host:        check.GetForge().GetHost(),
				Login:       check.GetForge().GetLogin(),
				Problem:     check.GetProblem(),
				ProblemKind: check.GetProblemKind(),
			}
		})

		return statuses, nil
	}

	env.IsPanelRunning = func(ctx context.Context) bool { return isPanelRunning(ctx, a) }
	env.ValidateKeybindings = ui.ValidateKeybindings

	return env, settings, nil
}

func (a *app) buildDoctorSettings() (doctor.Settings, error) {
	harnesses, err := doctor.DescribeHarnesses(a.harnesses)
	if err != nil {
		return doctor.Settings{}, err
	}

	termName, _ := config.LookupEnv("TERM")
	colorTerm, _ := config.LookupEnv("COLORTERM")

	return doctor.Settings{
		TmuxSocket:          a.env.TmuxSocket,
		InsideTmux:          a.env.Tmux,
		ConfigStack:         a.configStack,
		Sources:             a.configSources,
		Harnesses:           harnesses,
		DatabaseURL:         a.env.DatabaseURL,
		ReadOnlyDatabaseURL: a.env.ReadOnlyDatabaseURL,
		GetTenantID:         a.env.GetTenantID,
		Addr:                a.env.Addr,
		FocusKey:            ui.FocusKey,
		EngineIsRemote:      a.engine().IsRemote(),
		Version:             version.Version(),
		Commit:              version.Commit(),
		Term:                termName,
		ColorTerm:           colorTerm,
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
	report, err := a.runDoctor(ctx, false)
	if err != nil {
		return nil
	}

	failed := xslice.Filter(report.Findings, func(finding doctor.Finding) bool {
		return finding.Status == doctor.StatusFail
	})

	return xslice.Map(failed, func(finding doctor.Finding) errui.Finding {
		name := finding.Name
		if finding.Where != "" {
			name = finding.Where + " " + name
		}

		return errui.Finding{
			Name:   name,
			Reason: finding.Error,
			Fix:    finding.Fix,
		}
	})
}
