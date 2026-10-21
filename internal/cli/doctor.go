package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"komrad/config"
	"komrad/internal/doctor"
	"komrad/internal/errui"
	"komrad/internal/ui"
	"komrad/internal/version"
	"komrad/lib/xhttp"
	"komrad/lib/xslice"
	doctorv1 "komrad/proto/gen/doctor/v1"
	"komrad/proto/gen/doctor/v1/doctorv1connect"
	forgev1 "komrad/proto/gen/forge/v1"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/protobuf/types/known/emptypb"
)

var errReported = errors.New("reported")

func doctorCmd(state *app) *cobra.Command {
	var asJSON bool
	var color string

	cmd := &cobra.Command{
		Use:         "doctor",
		Annotations: map[string]string{survivesBadConfig: ""},
		Short:       "Check that this machine has everything komrad needs",
		Long: `Check that this machine has everything komrad needs, one line per dependency.

It checks tmux and its version, the shell tmux starts panes with, whether the
config file parses and names a harness that exists, the hooks and worktrees
directories and whether any worktree there is left without an agent, the database
and whether its schema is current, the read-only login the MCP query tools connect
as, the server on its address, and the CLI binary behind each harness and its
version. It repairs nothing, since a command that changed what it was describing
could not be trusted to say what is wrong.

It also reports what a bug report needs: the build of this binary, the operating
system with its release and architecture, and the terminal the panel is drawn in,
which inside tmux is asked of tmux, since tmux names itself to what it runs.

It also asks the terminal what it can draw. Every glyph the agents list uses is
written out and the cursor position queried, so what comes back is how far the
cursor moved rather than how wide the ink was. Inside tmux that answer comes
from tmux's own grid rather than from the terminal underneath it. Bold and
italic are read out of the terminfo entry of the terminal tmux paints to, and
colour depth out of the entry this process emits for, which is what an entry
claims rather than what a terminal does. Nothing is asked of a terminal that is
not there, and whether the font has a glyph at all is not tested.

Pointed at an engine with --engine, the checks about this machine run here and the
ones about the engine are asked of its server, which makes them on its own
machine; they are printed under an "on the engine" heading, and marked not checked
when the server cannot be reached.

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
				return errReported
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw report instead of the summary")
	cmd.Flags().StringVar(&color, "color", "auto", "auto, always, or never")

	return cmd
}

// runDoctor makes the checks about this machine and, when the engine is elsewhere, asks its
// server for the checks about that one; onTTY is whether a person typed the command.
func (a *app) runDoctor(ctx context.Context, onTTY bool) (doctor.Report, error) {
	env, settings, err := a.buildDoctor()
	if err != nil {
		return doctor.Report{}, err
	}

	// The command a person typed takes over the terminal, and no other does.
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

// askEngineDoctor is the checks the engine's server made on its own machine, read back into
// the doctor's own findings.
func (a *app) askEngineDoctor(ctx context.Context) ([]doctor.Finding, error) {
	base := a.getAPIBase()

	client := doctorv1connect.NewDoctorServiceClient(xhttp.NewClientOver(a.signTransport(xhttp.NewTransport()), apiCallTimeout), base)
	resp, err := client.RunDoctor(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		return nil, err
	}

	findings := make([]doctor.Finding, 0, len(resp.Msg.GetFindings()))
	for _, finding := range resp.Msg.GetFindings() {
		status, err := readStatus(finding.GetStatus())
		if err != nil {
			return nil, fmt.Errorf("failed to read the engine's %q finding: %w", finding.GetName(), err)
		}

		findings = append(findings, doctor.Finding{
			Name:        finding.GetName(),
			Description: finding.GetDescription(),
			Status:      status,
			Detail:      finding.GetDetail(),
			Code:        finding.GetCode(),
			Error:       finding.GetError(),
			Fix:         finding.GetFix(),
		})
	}

	return findings, nil
}

func readStatus(status doctorv1.Status) (doctor.Status, error) {
	switch status {
	case doctorv1.Status_STATUS_OK:
		return doctor.StatusOK, nil
	case doctorv1.Status_STATUS_WARN:
		return doctor.StatusWarn, nil
	case doctorv1.Status_STATUS_FAIL:
		return doctor.StatusFail, nil
	case doctorv1.Status_STATUS_SKIP:
		return doctor.StatusSkip, nil
	case doctorv1.Status_STATUS_UNSPECIFIED:
	}

	return "", fmt.Errorf("unknown status %s", status)
}

func (a *app) buildDoctor() (doctor.Env, doctor.Settings, error) {
	settings, err := a.buildDoctorSettings()
	if err != nil {
		return doctor.Env{}, doctor.Settings{}, err
	}

	env := doctor.NewEnv(settings)

	env.ProbeServer = func(ctx context.Context) (doctor.ServerStatus, error) {
		base := a.getAPIBase()

		found := probeServer(ctx, a.signTransport(xhttp.NewTransport()), base)
		if found.refusal != nil {
			return doctor.ServerStatus{}, found.refusal
		}

		return doctor.ServerStatus{
			Answering: found.answering,
			Ready:     found.ready,
			Version:   found.version,
			Commit:    found.commit,
		}, nil
	}

	env.CheckForges = func(ctx context.Context) ([]doctor.ForgeStatus, error) {
		base := a.getAPIBase()

		resp, err := a.forgeClient(base).CheckForges(ctx, connect.NewRequest(&emptypb.Empty{}))
		if err != nil {
			return nil, err
		}

		statuses := xslice.Map(resp.Msg.GetChecks(), func(check *forgev1.ForgeCheck) doctor.ForgeStatus {
			return doctor.ForgeStatus{
				Host:        check.GetForge().GetHost(),
				Login:       check.GetForge().GetLogin(),
				Kind:        check.GetForge().GetKind(),
				Method:      check.GetForge().GetMethod(),
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
	termProgram, _ := config.LookupEnv("TERM_PROGRAM")
	termProgramVersion, _ := config.LookupEnv("TERM_PROGRAM_VERSION")

	return doctor.Settings{
		TmuxSocket:          a.env.TmuxSocket,
		InsideTmux:          a.env.Tmux,
		ConfigStack:         a.configStack,
		Sources:             a.configSources,
		Harnesses:           harnesses,
		DatabaseURL:         a.env.DatabaseURL,
		ReadOnlyDatabaseURL: a.env.ReadOnlyDatabaseURL,
		EmbeddedPostgres:    a.env.EmbeddedPostgres,
		StateDir:            a.env.StateDir,
		Addr:                a.env.Addr,
		EngineURL:           a.env.URL,
		EngineIsRemote:      a.engine().IsRemote(),
		Term:                termName,
		ColorTerm:           colorTerm,
		TermProgram:         termProgram,
		TermProgramVersion:  termProgramVersion,
		Build:               version.Describe(version.Version(), version.Commit()),
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
