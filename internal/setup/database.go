package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/doctor"
)

// DatabaseStep gives this machine an engine, in the embedded Postgres unless the person runs a
// Postgres of their own, while database_url is left empty.
type DatabaseStep struct {
	File                   *ConfigFile
	Stack                  config.Stack
	HasEmbeddedPostgres    bool
	NoEmbeddedPostgresFlag bool
	OwnerURLFlag           string
	// CreateEmbeddedPostgres creates the embedded Postgres when there is none and starts it, reporting
	// whether it created one.
	CreateEmbeddedPostgres func(ctx context.Context) (bool, error)
	RemoveEmbeddedPostgres func(ctx context.Context) error
	MigrateAndCreateEngine func(ctx context.Context, ownerURL string) (string, error)
	created                bool
	pasted                 string
}

func (s *DatabaseStep) name() string {
	return "database"
}

func (s *DatabaseStep) observe(ctx context.Context, check checker) observation {
	report := check(ctx, doctor.CheckConfig, doctor.CheckEmbeddedPostgres, doctor.CheckDatabase, doctor.CheckSchema, doctor.CheckEngine)
	if finding := findingNamed(report, doctor.CheckConfig); finding.Status == doctor.StatusFail {
		return stopOn(&finding)
	}

	_, loaded, err := config.LoadWithReport(s.Stack)
	if err != nil {
		return observation{Verdict: verdictStop, Detail: err.Error()}
	}

	engine := findingNamed(report, doctor.CheckEngine)
	database := findingNamed(report, doctor.CheckDatabase)
	failed := firstFailing(report)

	switch {
	case !loaded.UsesEmbeddedPostgres() && failed != nil:
		return stopOn(failed)
	case failed == nil:
		return observation{Verdict: verdictDone, Detail: describeEngine(engine, database, loaded.UsesEmbeddedPostgres())}
	case !s.takesOwnerURL():
		return observation{Verdict: verdictRepair, Detail: "no database is set up"}
	case !s.File.isWritable():
		return observation{Verdict: verdictStop, Detail: "no database is set up; " + s.File.describeRefusal("database_url under [connection]") + ", or move it aside and run setup again"}
	}

	return observation{Verdict: verdictAsk, Detail: "no database is set up"}
}

// takesOwnerURL reports whether the engine goes in a Postgres the person runs rather than the embedded one.
func (s *DatabaseStep) takesOwnerURL() bool {
	return !s.HasEmbeddedPostgres || s.NoEmbeddedPostgresFlag || s.OwnerURLFlag != ""
}

func (s *DatabaseStep) answerUnattended() error {
	switch {
	case s.OwnerURLFlag != "":
		s.pasted = s.OwnerURLFlag
	case !s.HasEmbeddedPostgres:
		return fmt.Errorf("%w: this build carries no embedded Postgres, so pass --owner-database-url with a login that owns your database", errNeedsAnswer)
	default:
		return fmt.Errorf("%w: pass --owner-database-url with a login that owns your database", errNeedsAnswer)
	}

	return nil
}

func (s *DatabaseStep) repair(ctx context.Context) error {
	if s.takesOwnerURL() {
		return s.repairOwnerURL(ctx)
	}

	created, err := s.CreateEmbeddedPostgres(ctx)
	if err != nil {
		return fmt.Errorf("failed to set up the embedded Postgres: %w", err)
	}
	s.created = created

	cfg, err := config.LoadStack(s.Stack)
	if err != nil {
		return s.undo(ctx, fmt.Errorf("failed to read the config: %w", err))
	}

	if _, err := s.MigrateAndCreateEngine(ctx, cfg.Connection.OwnerDatabaseURL); err != nil {
		return s.undo(ctx, fmt.Errorf("failed to create the engine: %w", err))
	}

	return nil
}

// repairOwnerURL creates the engine in the Postgres the owner's url names and writes both urls to the config.
func (s *DatabaseStep) repairOwnerURL(ctx context.Context) error {
	ownerURL := strings.TrimSpace(s.pasted)

	databaseURL, err := s.MigrateAndCreateEngine(ctx, ownerURL)
	if err != nil {
		return fmt.Errorf("failed to create the engine: %w", err)
	}

	if err := s.File.write(func(a *answers) {
		a.OwnerDatabaseURL = ownerURL
		a.DatabaseURL = databaseURL
	}); err != nil {
		return err
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

// undo removes the embedded Postgres this run created when creating the engine in it failed.
func (s *DatabaseStep) undo(ctx context.Context, err error) error {
	if !s.created {
		return err
	}

	return errors.Join(err, s.RemoveEmbeddedPostgres(context.WithoutCancel(ctx)))
}
