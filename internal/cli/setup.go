package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"komrad/config"
	"komrad/internal/doctor"
	"komrad/internal/forge"
	"komrad/internal/logging"
	"komrad/internal/postgres"
	"komrad/internal/setup"
	forgev1 "komrad/proto/gen/forge/v1"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// setupEngine is the engine setup makes for this machine.
const setupEngine = "default"

type setupFlags struct {
	harness            string
	noEmbeddedPostgres bool
	ownerURL           string
}

func setupCmd(state *app) *cobra.Command {
	var noInput bool
	var flags setupFlags

	cmd := &cobra.Command{
		Use:         "setup",
		Annotations: map[string]string{survivesBadConfig: ""},
		Short:       "Set this machine up for komrad, asking for whatever is missing",
		Long: `Set this machine up for komrad, asking for whatever is missing.

Each step asks the doctor whether its part of the machine is ready, and only when it is not
does it ask a question and change something, then asks the doctor again. Nothing records
which steps ran, so running it again after stopping part way finds the finished steps
already done. It ends on the doctor's full report.

Setup writes the config file only when there is none, from a config template filled with
your answers. It never changes a config file that already exists: a step that would stops and
names the setting to change there.

With --no-input, or when stdin is not a terminal, nothing is asked: a step that needs an
answer takes it from a flag, and names the flag when none was given.

The database step runs while database_url is left empty. It creates the embedded Postgres
under the state directory without asking, migrates it, and makes an engine named default,
writing no connection settings. With --no-embedded-postgres it takes the url of a login that
owns a Postgres you already run instead, from --owner-database-url or by asking, and writes
the [connection] settings.

The forge step connects github.com or gitlab.com with a pasted token once the engine is
running, and skips until then. Unattended it skips, and komrad forge connect connects any
forge after setup.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			steps, err := state.setupSteps(flags)
			if err != nil {
				return err
			}

			check := func(ctx context.Context, names ...string) doctor.Report {
				if err := state.reloadConnection(); err != nil {
					logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to reload the config, so the checks read it as it was", logging.Err(err)...)
				}

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
			if ctx.Err() != nil {
				return fmt.Errorf("failed to finish setup: %w", context.Cause(ctx))
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
				return errReported
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&noInput, "no-input", false, "ask nothing, taking every answer from a flag")
	cmd.Flags().StringVar(&flags.harness, "harness", "", "the harness new agents run, when the default one is not on PATH")
	cmd.Flags().BoolVar(&flags.noEmbeddedPostgres, "no-embedded-postgres", false, "keep the database in a Postgres you already run, when no database is set up")
	cmd.Flags().StringVar(&flags.ownerURL, "owner-database-url", "", "the url of a login that owns a Postgres you already run, when no database is set up")

	return cmd
}

func (a *app) setupSteps(flags setupFlags) ([]setup.Step, error) {
	layers, err := config.ResolveLayers(config.Stack{Paths: a.configStack.Paths})
	if err != nil {
		return nil, err
	}
	file := &setup.ConfigFile{Path: layers[len(layers)-1].Path}

	return []setup.Step{
		&setup.ConfigStep{File: file},
		&setup.DatabaseStep{
			File:                   file,
			Stack:                  a.configStack,
			HasEmbeddedPostgres:    postgres.IsEmbedded(),
			NoEmbeddedPostgresFlag: flags.noEmbeddedPostgres,
			OwnerURLFlag:           flags.ownerURL,
			CreateEmbeddedPostgres: a.createEmbeddedPostgres,
			RemoveEmbeddedPostgres: a.removeEmbeddedPostgres,
			MigrateAndCreateEngine: migrateAndCreateEngine,
		},
		&setup.TmuxStep{},
		&setup.HarnessStep{
			File:      file,
			Stack:     a.configStack,
			Harnesses: a.harnesses.IDs(),
			Flag:      flags.harness,
		},
		&setup.ContextStep{Stack: a.configStack},
		&setup.AutoSetupStep{File: file, Stack: a.configStack},
		&setup.ForgeStep{
			Connect: a.connectForge,
		},
	}, nil
}

func (a *app) connectForge(ctx context.Context, host string, token forge.Secret) error {
	return a.withAPI(ctx, func(ctx context.Context, base string) error {
		_, err := a.forgeClient(base).ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{
			Host:   host,
			Method: forge.MethodToken,
			Token:  tokenForTheWire(token),
		}))

		return err
	})
}

// reloadConnection reads the connection settings again, so checks after a step see what it wrote.
func (a *app) reloadConnection() error {
	cfg, report, err := config.LoadWithReport(a.configStack)
	if err != nil {
		return err
	}

	env, err := loadEnvironment(cfg, report)
	if err != nil {
		return err
	}

	a.env = env
	a.configSources = report.Sources

	return nil
}

// createEmbeddedPostgres creates the embedded Postgres when there is none and starts it.
func (a *app) createEmbeddedPostgres(ctx context.Context) (bool, error) {
	return createOrStartEmbeddedPostgres(ctx, config.GetEmbeddedPostgresDir(a.env.StateDir))
}

// removeEmbeddedPostgres deletes the embedded Postgres createEmbeddedPostgres set up.
func (a *app) removeEmbeddedPostgres(ctx context.Context) error {
	return postgres.RemoveCluster(ctx, config.GetEmbeddedPostgresDir(a.env.StateDir))
}
