package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/doctor"

	"charm.land/huh/v2"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	choiceRunPostgres = "run"
	choiceOwnerURL    = "owner"
)

// DatabaseStep gives this machine an engine, in a Postgres komrad runs or one the person already
// runs, while database_url is left at its default.
type DatabaseStep struct {
	File           *ConfigFile
	Stack          config.Stack
	Bundled        bool
	RunFlag        bool
	OwnerURLFlag   string
	CreatePostgres func(ctx context.Context) (string, error)
	RemovePostgres func(ctx context.Context) error
	JoinEngine     func(ctx context.Context, ownerURL string) (string, error)
	choice         string
	pasted         string
}

func (s *DatabaseStep) name() string {
	return "database"
}

func (s *DatabaseStep) observe(ctx context.Context, check checker) observation {
	report := check(ctx, doctor.CheckConfig, doctor.CheckPostgres, doctor.CheckDatabase, doctor.CheckSchema, doctor.CheckEngine)
	for _, name := range []string{doctor.CheckConfig, doctor.CheckPostgres} {
		if finding := findingNamed(report, name); finding.Status == doctor.StatusFail {
			return stopOn(&finding)
		}
	}

	cfg, loaded, err := config.LoadWithReport(s.Stack)
	if err != nil {
		return observation{Verdict: verdictStop, Detail: err.Error()}
	}

	if loaded.IsDefault("database_url") {
		if !s.File.isWritable() {
			return observation{Verdict: verdictStop, Detail: "no database is set up; " + s.File.refusal("database_url under [connection]") + ", or move it aside and run setup again"}
		}

		return observation{Verdict: verdictAsk, Detail: "no database is set up"}
	}
	if failed := firstFailing(report); failed != nil {
		return stopOn(failed)
	}

	database := findingNamed(report, doctor.CheckDatabase)
	engine := findingNamed(report, doctor.CheckEngine)
	if cfg.Connection.RunPostgres {
		return observation{Verdict: verdictDone, Detail: "engine " + engine.Detail + " in the Postgres komrad runs"}
	}

	return observation{Verdict: verdictDone, Detail: "engine " + engine.Detail + " in " + database.Detail}
}

func (s *DatabaseStep) question() *huh.Form {
	options := []huh.Option[string]{huh.NewOption("A Postgres I already run. Paste its URL.", choiceOwnerURL)}
	if s.Bundled {
		options = append([]huh.Option[string]{huh.NewOption("On this machine. Komrad runs Postgres for you.", choiceRunPostgres)}, options...)
	}
	s.choice = options[0].Value

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Where should komrad keep its database?").
				Options(options...).
				Value(&s.choice),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Owner URL").
				Description("A login that owns the database. Komrad migrates with it and makes the laptop engine's login.").
				EchoMode(huh.EchoModePassword).
				Validate(requireOwnerURL).
				Value(&s.pasted),
		).WithHideFunc(func() bool { return s.choice != choiceOwnerURL }),
	)
}

func requireOwnerURL(pasted string) error {
	if strings.TrimSpace(pasted) == "" {
		return errors.New("paste a postgres:// url")
	}
	if _, err := pgconn.ParseConfig(strings.TrimSpace(pasted)); err != nil {
		return errors.New("that is not a url Postgres accepts")
	}

	return nil
}

func (s *DatabaseStep) answerUnattended() error {
	switch {
	case s.RunFlag:
		s.choice = choiceRunPostgres
	case s.OwnerURLFlag != "":
		s.choice = choiceOwnerURL
		s.pasted = s.OwnerURLFlag
	case s.Bundled:
		return fmt.Errorf("%w: pass --run-postgres, or --owner-database-url with a login that owns your database", errNeedsAnswer)
	default:
		return fmt.Errorf("%w: pass --owner-database-url with a login that owns your database", errNeedsAnswer)
	}

	return nil
}

func (s *DatabaseStep) repair(ctx context.Context) error {
	ownerURL := strings.TrimSpace(s.pasted)
	runPostgres := s.choice == choiceRunPostgres
	if runPostgres {
		created, err := s.CreatePostgres(ctx)
		if err != nil {
			return fmt.Errorf("failed to set up Postgres: %w", err)
		}
		ownerURL = created
	}

	databaseURL, err := s.JoinEngine(ctx, ownerURL)
	if err != nil {
		return s.undo(ctx, runPostgres, fmt.Errorf("failed to join the database: %w", err))
	}

	if err := s.File.write(func(a *answers) {
		a.RunPostgres = runPostgres
		a.OwnerDatabaseURL = ownerURL
		a.DatabaseURL = databaseURL
	}); err != nil {
		return s.undo(ctx, runPostgres, err)
	}

	cfg, err := config.LoadStack(s.Stack)
	if err != nil {
		return fmt.Errorf("failed to read the config back: %w", err)
	}
	if cfg.Connection.DatabaseURL != databaseURL {
		return fmt.Errorf("wrote database_url to %s, but a later config layer or KOMRAD_DATABASE_URL sets it", shortenHome(s.File.Path))
	}

	return nil
}

// undo removes the Postgres this run created when it failed before the config held its owner.
func (s *DatabaseStep) undo(ctx context.Context, created bool, err error) error {
	if !created {
		return err
	}

	return errors.Join(err, s.RemovePostgres(context.WithoutCancel(ctx)))
}
