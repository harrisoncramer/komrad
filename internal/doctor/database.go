package doctor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"weave/internal/dsn"

	"github.com/jackc/pgx/v5/pgconn"
)

// The SQLSTATEs worth telling apart. Postgres reports each as a connection failure, and
// only the code says which one it was.
const (
	pgAuthFailed      = "28P01"
	pgAuthUnspecified = "28000"
	pgDatabaseMissing = "3D000"
)

// The ways the database can be wrong, each its own code since each needs a different fix,
// though from the panel they all look like a server that would not start.
var (
	ErrDatabaseURLInvalid = &Problem{
		Code:    "database_url_invalid",
		Message: "the database url is not a connection string Postgres accepts",
	}
	ErrDatabaseUnreachable = &Problem{
		Code:    "database_unreachable",
		Message: "nothing answered",
	}
	ErrDatabaseAuthFailed = &Problem{
		Code:    "database_auth_failed",
		Message: "the database refused the user or password in the database url",
	}
	ErrDatabaseMissing = &Problem{
		Code:    "database_missing",
		Message: "the Postgres server is running but has no database of that name",
	}
	ErrSchemaBehind = &Problem{
		Code:    "schema_behind",
		Message: "the database is missing migrations this build ships",
	}
	ErrSchemaUnreadable = &Problem{
		Code:    "schema_unreadable",
		Message: "the applied migrations could not be read",
	}
	ErrReadOnlyLoginShared = &Problem{
		Code:    "readonly_login_shared",
		Message: "WEAVE_READONLY_DATABASE_URL names the same login as DATABASE_URL",
	}
	ErrReadOnlyLoginUnset = &Problem{
		Code:    "readonly_login_unset",
		Message: "there is no read-only login, and the database url is not one it can be derived from",
	}
	ErrReadOnlyLoginElsewhere = &Problem{
		Code:    "readonly_login_elsewhere",
		Message: "the MCP query tools read a different database than the panel writes to",
	}
	ErrReadOnlyLoginUnusable = &Problem{
		Code:    "readonly_login_unusable",
		Message: "the least-privilege login the MCP tools connect as does not work",
	}
)

// Database asserts Postgres is there and will let weave in.
func Database() Check {
	return Check{
		Name:        "database",
		Description: "every agent, workflow, and schedule weave knows about lives in Postgres",
		Run: func(ctx context.Context, env Env) Result {
			// Parsed before connecting so a malformed string is its own answer rather
			// than a failure to reach an address that was never an address.
			if _, err := pgconn.ParseConfig(env.DatabaseURL); err != nil {
				return fail(
					fmt.Errorf("%w: %w", ErrDatabaseURLInvalid, err),
					"",
					"set connection.database_url, or WEAVE_DATABASE_URL, to a postgres:// url",
				)
			}

			target := dsn.Redact(env.DatabaseURL) + describeSource(env, "database_url")

			if err := env.PingDatabase(ctx, env.DatabaseURL); err != nil {
				return classifyDatabaseFailure(err, env.DatabaseURL, target)
			}

			return pass(target)
		},
	}
}

// Schema asserts the database has every migration this build ships; being behind is not a
// failure the user has to fix, since the service migrates on the way up.
func Schema() Check {
	return Check{
		Name:        "schema",
		Description: "the tables weave reads have to match the build reading them",
		Needs:       []string{"database"},
		Run: func(ctx context.Context, env Env) Result {
			pending, err := env.ReadPendingMigrations(ctx, env.DatabaseURL)
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrSchemaUnreadable, err), "", "weave migrate")
			}

			if len(pending) > 0 {
				return warn(
					fmt.Errorf("%w: %s not applied yet", ErrSchemaBehind, describeMigrationCount(len(pending))),
					"",
				)
			}

			return pass("up to date")
		},
	}
}

// ReadOnlyLogin asserts the least-privilege login exists and can connect; the service opens
// a second pool as this login and refuses to start when it cannot, so this check connects
// the same way the service does rather than something close to it.
func ReadOnlyLogin() Check {
	return Check{
		Name:        "readonly login",
		Description: "the MCP query tools read through a login that can only read",
		Needs:       []string{"database"},
		Run: func(ctx context.Context, env Env) Result {
			// An empty url is not a missing setting the way an empty cache directory is:
			// Postgres reads it as every default, so the pool would open as whoever is
			// running weave rather than refuse.
			if env.ReadOnlyDatabaseURL == "" {
				return fail(
					ErrReadOnlyLoginUnset,
					"",
					"set WEAVE_READONLY_DATABASE_URL to a postgres:// url, then run weave readonly-role",
				)
			}

			// Checked before connecting, since one login for both would connect cleanly
			// and hand the MCP endpoint the writable role.
			if env.ReadOnlyDatabaseURL == env.DatabaseURL {
				return fail(
					ErrReadOnlyLoginShared,
					dsn.Redact(env.ReadOnlyDatabaseURL),
					"point WEAVE_READONLY_DATABASE_URL at its own login, then run weave readonly-role",
				)
			}

			if err := env.ConnectReadOnly(ctx, env.ReadOnlyDatabaseURL); err != nil {
				return fail(
					fmt.Errorf("%w: %s", ErrReadOnlyLoginUnusable, describeDatabaseReason(err)),
					dsn.Redact(env.ReadOnlyDatabaseURL),
					"weave readonly-role",
				)
			}

			// Reported rather than refused: an address weave cannot match up may still be
			// the same database behind a pooler or a second name for the host, and only
			// whoever wrote the url can say.
			if where := describeStoreMismatch(env.DatabaseURL, env.ReadOnlyDatabaseURL); where != "" {
				return warn(
					fmt.Errorf("%w: %s", ErrReadOnlyLoginElsewhere, where),
					dsn.Redact(env.ReadOnlyDatabaseURL),
				)
			}

			return pass(dsn.Redact(env.ReadOnlyDatabaseURL) + describeSource(env, "readonly_database_url"))
		},
	}
}

// classifyDatabaseFailure names what Postgres objected to, and what to do about it, since a
// server that is not running, a wrong password, and a database that was never created each
// need something different done.
func classifyDatabaseFailure(err error, url, detail string) Result {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgAuthFailed, pgAuthUnspecified:
			return fail(fmt.Errorf("%w: %s", ErrDatabaseAuthFailed, pgErr.Message), detail,
				"check the user and password in the database url")
		case pgDatabaseMissing:
			return fail(fmt.Errorf("%w: %s", ErrDatabaseMissing, pgErr.Message), detail,
				"createdb "+readDatabaseName(url)+", or point the database url at one that exists")
		}
	}

	// No mention of docker or mise: most people who read this line installed a binary, have
	// neither, and their Postgres is wherever they put it.
	return fail(
		fmt.Errorf("%w %s: %s", ErrDatabaseUnreachable, describeHostPort(url), describeNetworkReason(err)),
		detail,
		"start Postgres "+describeHostPort(url)+", or point the database url at one that is running",
	)
}

// describeStoreMismatch names the two addresses when the pair of logins does not land on
// one database, and is empty when it does or when either url is unreadable, since that
// failure is reported elsewhere. The login is left out of the comparison on purpose, since
// two logins is the point of the pair and what has to match is the rows they reach.
func describeStoreMismatch(databaseURL, readOnlyURL string) string {
	writable, err := pgconn.ParseConfig(databaseURL)
	if err != nil {
		return ""
	}

	readOnly, err := pgconn.ParseConfig(readOnlyURL)
	if err != nil {
		return ""
	}

	if writable.Host == readOnly.Host && writable.Port == readOnly.Port && writable.Database == readOnly.Database {
		return ""
	}

	return fmt.Sprintf("the database url is %s and this is %s",
		describeStore(writable), describeStore(readOnly))
}

// describeStore is the database a config reaches, with the login left out.
func describeStore(cfg *pgconn.Config) string {
	return cfg.Host + ":" + strconv.Itoa(int(cfg.Port)) + "/" + cfg.Database
}

// describeDatabaseReason is what went wrong in the words Postgres or the network used.
func describeDatabaseReason(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Message
	}

	return describeNetworkReason(err)
}

// describeNetworkReason is why the dial did not land, in the few words that say it: pgx
// reports the whole attempt, which for a host resolving to both IPv4 and IPv6 is two dial
// errors and the connection string again, when this is one fact on one row.
func describeNetworkReason(err error) string {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		return opErr.Err.Error()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}

	return err.Error()
}

// describeHostPort is where the connection was attempted, since the reason alone does not
// name it.
func describeHostPort(url string) string {
	cfg, err := pgconn.ParseConfig(url)
	if err != nil {
		return "at the configured address"
	}

	return "at " + cfg.Host + ":" + strconv.Itoa(int(cfg.Port))
}

// readDatabaseName is the database the url names, so a fix can say which one to create.
func readDatabaseName(url string) string {
	cfg, err := pgconn.ParseConfig(url)
	if err != nil {
		return "the database"
	}

	return cfg.Database
}

// describeMigrationCount writes a number of migrations as a phrase, so the line reads as a
// sentence rather than a count with a noun stuck to it.
func describeMigrationCount(pending int) string {
	if pending == 1 {
		return "1 migration"
	}

	return strconv.Itoa(pending) + " migrations"
}
