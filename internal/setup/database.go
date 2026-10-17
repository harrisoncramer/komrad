package setup

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
// runs, while database_url is left at its default and nothing answers there.
type DatabaseStep struct {
	Path           string
	Stack          config.Stack
	Bundled        bool
	RunFlag        bool
	OwnerURLFlag   string
	CreatePostgres func(ctx context.Context) (string, error)
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

	database := findingNamed(report, doctor.CheckDatabase)
	if database.Status == doctor.StatusFail && loaded.IsDefault("database_url") {
		return observation{Verdict: verdictAsk, Detail: "no database is set up"}
	}
	if failed := firstFailing(report); failed != nil {
		return stopOn(failed)
	}

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
	var keys []tableKey
	if s.choice == choiceRunPostgres {
		created, err := s.CreatePostgres(ctx)
		if err != nil {
			return err
		}
		ownerURL = created
		keys = append(keys, tableKey{Key: "run_postgres", Literal: "true"})
	}

	keys = append(keys, tableKey{Key: "owner_database_url", Literal: strconv.Quote(ownerURL)})
	if err := setTableKeys(s.Path, "connection", keys); err != nil {
		return err
	}

	databaseURL, err := s.JoinEngine(ctx, ownerURL)
	if err != nil {
		return err
	}

	if err := setTableKeys(s.Path, "connection", []tableKey{{Key: "database_url", Literal: strconv.Quote(databaseURL)}}); err != nil {
		return err
	}

	cfg, err := config.LoadStack(s.Stack)
	if err != nil {
		return err
	}
	if cfg.Connection.DatabaseURL != databaseURL {
		return fmt.Errorf("wrote database_url to %s, but a later config layer or KOMRAD_DATABASE_URL sets it", shortenHome(s.Path))
	}

	return nil
}
