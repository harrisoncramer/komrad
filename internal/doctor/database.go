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

const (
	pgAuthFailed      = "28P01"
	pgAuthUnspecified = "28000"
	pgDatabaseMissing = "3D000"
)

var (
	ErrDatabaseURLInvalid = &ProblemError{
		Code:    "database_url_invalid",
		Message: "the database url is not a connection string Postgres accepts",
	}
	ErrDatabaseUnreachable = &ProblemError{
		Code:    "database_unreachable",
		Message: "nothing answered",
	}
	ErrDatabaseAuthFailed = &ProblemError{
		Code:    "database_auth_failed",
		Message: "the database refused the user or password in the database url",
	}
	ErrDatabaseMissing = &ProblemError{
		Code:    "database_missing",
		Message: "the Postgres server is running but has no database of that name",
	}
	ErrSchemaBehind = &ProblemError{
		Code:    "schema_behind",
		Message: "the database is missing migrations this build ships",
	}
	ErrSchemaUnreadable = &ProblemError{
		Code:    "schema_unreadable",
		Message: "the applied migrations could not be read",
	}
	ErrReadOnlyLoginShared = &ProblemError{
		Code:    "readonly_login_shared",
		Message: "WEAVE_READONLY_DATABASE_URL names the same login as DATABASE_URL",
	}
	ErrReadOnlyLoginUnset = &ProblemError{
		Code:    "readonly_login_unset",
		Message: "there is no read-only login, and the database url is not one it can be derived from",
	}
	ErrReadOnlyLoginElsewhere = &ProblemError{
		Code:    "readonly_login_elsewhere",
		Message: "the MCP query tools read a different database than the panel writes to",
	}
	ErrReadOnlyLoginUnusable = &ProblemError{
		Code:    "readonly_login_unusable",
		Message: "the least-privilege login the MCP tools connect as does not work",
	}
)

// newDatabaseCheck asserts Postgres is there and will let weave in.
func newDatabaseCheck() Check {
	return Check{
		Name:        "database",
		Description: "every agent, workflow, and schedule weave knows about lives in Postgres",
		Run: func(ctx context.Context, env Env) Result {
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

// newSchemaCheck asserts the database has every migration this build ships; being behind is not a
// failure the user has to fix, since the service migrates on the way up.
func newSchemaCheck() Check {
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

// newReadOnlyLoginCheck asserts the least-privilege login exists and can connect; the service opens
// a second pool as this login and refuses to start when it cannot, so this check connects
// the same way the service does rather than something close to it.
func newReadOnlyLoginCheck() Check {
	return Check{
		Name:        "readonly login",
		Description: "the MCP query tools read through a login that can only read",
		Needs:       []string{"database"},
		Run: func(ctx context.Context, env Env) Result {
			if env.ReadOnlyDatabaseURL == "" {
				return fail(
					ErrReadOnlyLoginUnset,
					"",
					"set WEAVE_READONLY_DATABASE_URL to a postgres:// url, then run weave readonly-role",
				)
			}

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

	return fail(
		fmt.Errorf("%w %s: %s", ErrDatabaseUnreachable, describeHostPort(url), describeNetworkReason(err)),
		detail,
		"start Postgres "+describeHostPort(url)+", or point the database url at one that is running",
	)
}

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

func describeStore(cfg *pgconn.Config) string {
	return cfg.Host + ":" + strconv.Itoa(int(cfg.Port)) + "/" + cfg.Database
}

func describeDatabaseReason(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Message
	}

	return describeNetworkReason(err)
}

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

func describeHostPort(url string) string {
	cfg, err := pgconn.ParseConfig(url)
	if err != nil {
		return "at the configured address"
	}

	return "at " + cfg.Host + ":" + strconv.Itoa(int(cfg.Port))
}

func readDatabaseName(url string) string {
	cfg, err := pgconn.ParseConfig(url)
	if err != nil {
		return "the database"
	}

	return cfg.Database
}

func describeMigrationCount(pending int) string {
	if pending == 1 {
		return "1 migration"
	}

	return strconv.Itoa(pending) + " migrations"
}
