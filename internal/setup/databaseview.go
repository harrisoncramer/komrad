package setup

import (
	"errors"
	"strings"

	"komrad/internal/doctor"

	"charm.land/huh/v2"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *DatabaseStep) question() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Owner URL").
				Description("A login that owns the Postgres komrad keeps its database in. Komrad migrates with it and makes the default engine's login.").
				EchoMode(huh.EchoModePassword).
				Validate(requireOwnerURL).
				Value(&s.pasted),
		),
	)
}

func requireOwnerURL(pasted string) error {
	ownerURL := strings.TrimSpace(pasted)
	if ownerURL == "" {
		return errors.New("paste a postgres:// url")
	}
	if _, err := pgconn.ParseConfig(ownerURL); err != nil {
		return errors.New("that is not a url Postgres accepts")
	}

	return nil
}

// describeEngine is the line setup shows for an engine in the embedded Postgres or in the database it names.
func describeEngine(engine, database doctor.Finding, isEmbeddedPostgres bool) string {
	if isEmbeddedPostgres {
		return "engine " + engine.Detail + " in the embedded Postgres"
	}

	return "engine " + engine.Detail + " in " + database.Detail
}
