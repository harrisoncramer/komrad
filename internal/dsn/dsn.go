// Package dsn writes a Postgres connection string in a form that is safe to show.
//
// It is its own package because two callers need it and neither should import the other:
// doctor prints connection strings in a report a user pastes into an issue, and the config
// service hands the whole config back over RPC and to the MCP surface, where a model reads it.
package dsn

import (
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

// Redact is the connection string with the password taken out, keeping the login and the
// address because those are the parts worth reading. Somebody diagnosing the wrong database
// needs to know which host and which user, and never needs the secret.
//
// A string that will not parse is reported as such rather than passed through, since passing
// it through would leak the password of anything this failed to understand.
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
