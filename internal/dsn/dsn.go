// Package dsn takes the password out of a Postgres connection string, so the URL can be shown
// in a log line, an error, or the doctor's report without carrying the secret with it.
package dsn

import (
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

// Redact is the connection string with the password taken out, reporting a string it cannot
// parse rather than passing it through and leaking the password.
func Redact(url string) string {
	if url == "" {
		return ""
	}

	cfg, err := pgconn.ParseConfig(url)
	if err != nil {
		return "(unparseable)"
	}

	return "postgres://" + cfg.User + "@" + cfg.Host + ":" + strconv.Itoa(int(cfg.Port)) + "/" + cfg.Database
}
