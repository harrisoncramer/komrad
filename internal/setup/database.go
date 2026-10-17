package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/doctor"
)

const (
	choiceEmbeddedPostgres = "run"
	choiceOwnerURL         = "owner"
)

// DatabaseStep gives this machine an engine, in the embedded Postgres or one the person already
// runs, while database_url is left at its default.
type DatabaseStep struct {
	File                   *ConfigFile
	Stack                  config.Stack
	Bundled                bool
	EmbeddedPostgresFlag   bool
	OwnerURLFlag           string
	CreateEmbeddedPostgres func(ctx context.Context) (string, error)
	RemoveEmbeddedPostgres func(ctx context.Context) error
	MigrateAndCreateEngine func(ctx context.Context, ownerURL string) (string, error)
	choice                 string
	pasted                 string
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

	engine := findingNamed(report, doctor.CheckEngine)
	database := findingNamed(report, doctor.CheckDatabase)

	return observation{Verdict: verdictDone, Detail: describeEngine(engine, database, cfg.Connection.EmbeddedPostgres)}
}

func (s *DatabaseStep) answerUnattended() error {
	switch {
	case s.EmbeddedPostgresFlag:
		s.choice = choiceEmbeddedPostgres
	case s.OwnerURLFlag != "":
		s.choice = choiceOwnerURL
		s.pasted = s.OwnerURLFlag
	case s.Bundled:
		return fmt.Errorf("%w: pass --embedded-postgres, or --owner-database-url with a login that owns your database", errNeedsAnswer)
	default:
		return fmt.Errorf("%w: pass --owner-database-url with a login that owns your database", errNeedsAnswer)
	}

	return nil
}

func (s *DatabaseStep) repair(ctx context.Context) error {
	ownerURL := strings.TrimSpace(s.pasted)
	embeddedPostgres := s.choice == choiceEmbeddedPostgres
	if embeddedPostgres {
		created, err := s.CreateEmbeddedPostgres(ctx)
		if err != nil {
			return fmt.Errorf("failed to set up Postgres: %w", err)
		}
		ownerURL = created
	}

	databaseURL, err := s.MigrateAndCreateEngine(ctx, ownerURL)
	if err != nil {
		return s.undo(ctx, fmt.Errorf("failed to create the engine: %w", err))
	}

	if err := s.File.write(func(a *answers) {
		a.EmbeddedPostgres = embeddedPostgres
		a.OwnerDatabaseURL = ownerURL
		a.DatabaseURL = databaseURL
	}); err != nil {
		return s.undo(ctx, err)
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
func (s *DatabaseStep) undo(ctx context.Context, err error) error {
	if s.choice != choiceEmbeddedPostgres {
		return err
	}

	return errors.Join(err, s.RemoveEmbeddedPostgres(context.WithoutCancel(ctx)))
}
