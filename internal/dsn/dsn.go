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
