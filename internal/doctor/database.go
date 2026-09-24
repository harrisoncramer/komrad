package doctor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"komrade/internal/dsn"
	"komrade/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	pgAuthFailed      = "28P01"
	pgAuthUnspecified = "28000"
	pgDatabaseMissing = "3D000"
)

var (
	errDatabaseURLInvalid = &ProblemError{
		Code:    "database_url_invalid",
		Message: "the database url is not a connection string Postgres accepts",
	}
	errDatabaseUnreachable = &ProblemError{
		Code:    "database_unreachable",
		Message: "nothing answered",
	}
	errDatabaseAuthFailed = &ProblemError{
		Code:    "database_auth_failed",
		Message: "the database refused the user or password in the database url",
	}
	errDatabaseMissing = &ProblemError{
		Code:    "database_missing",
		Message: "the Postgres server is running but has no database of that name",
	}
	errSchemaBehind = &ProblemError{
		Code:    "schema_behind",
		Message: "the database is missing migrations this build ships",
	}
	errSchemaUnreadable = &ProblemError{
		Code:    "schema_unreadable",
		Message: "the applied migrations could not be read",
	}
	errNotAnEngine = &ProblemError{
		Code:    "not_an_engine",
		Message: "the database url's login belongs to no engine",
	}
	errEngineUnreadable = &ProblemError{
		Code:    "engine_unreadable",
		Message: "which engine the database url's login belongs to could not be read",
	}
	errReadOnlyLoginShared = &ProblemError{
		Code:    "readonly_login_shared",
		Message: "KOMRADE_READONLY_DATABASE_URL names the same login as DATABASE_URL",
	}
	errReadOnlyLoginUnset = &ProblemError{
		Code:    "readonly_login_unset",
		Message: "there is no read-only login, and the database url is not one it can be derived from",
	}
	errReadOnlyLoginElsewhere = &ProblemError{
		Code:    "readonly_login_elsewhere",
		Message: "the MCP query tools read a different database than the panel writes to",
	}
	errReadOnlyLoginUnusable = &ProblemError{
		Code:    "readonly_login_unusable",
		Message: "the least-privilege login the MCP tools connect as does not work",
	}
)

// newDatabaseCheck asserts Postgres is there and will let komrade in.
func newDatabaseCheck() Check {
	return Check{
		Name:        "database",
		Description: "every agent, workflow, and schedule komrade knows about lives in Postgres",
		Run: func(ctx context.Context, env Env) Result {
			if _, err := pgconn.ParseConfig(env.DatabaseURL); err != nil {
				return fail(
					fmt.Errorf("%w: %w", errDatabaseURLInvalid, err),
					"",
					"set connection.database_url, or KOMRADE_DATABASE_URL, to a postgres:// url",
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

// newSchemaCheck asserts the database has every migration this build ships, taking a database
// behind the binary as no failure of the user's.
func newSchemaCheck() Check {
	return Check{
		Name:        "schema",
		Description: "the tables komrade reads have to match the build reading them",
		Needs:       []string{"database"},
		Run: func(ctx context.Context, env Env) Result {
			pending, err := env.ReadPendingMigrations(ctx, env.DatabaseURL)
			if err != nil {
				return fail(fmt.Errorf("%w: %w", errSchemaUnreadable, err), "", "komrade migrate")
			}

			if len(pending) > 0 {
				return fail(
					fmt.Errorf("%w: %s not applied yet", errSchemaBehind, describeMigrationCount(len(pending))),
					"",
					"komrade migrate, with owner_database_url naming the login that owns the schema",
				)
			}

			return pass("up to date")
		},
	}
}

// newEngineCheck asserts database_url is a login that belongs to an engine.
func newEngineCheck() Check {
	return Check{
		Name:        "engine",
		Description: "an engine connects as a login of its own, which is what keeps its rows apart",
		Needs:       []string{"database", "schema"},
		Run: func(ctx context.Context, env Env) Result {
			name, err := env.ReadEngine(ctx, env.DatabaseURL)
			if errors.Is(err, store.ErrNotAnEngine) {
				return fail(errNotAnEngine, dsn.Redact(env.DatabaseURL), "komrade engine ensure NAME, then set database_url to the url it prints")
			}
			if err != nil {
				return fail(fmt.Errorf("%w: %s", errEngineUnreadable, describeDatabaseReason(err)), dsn.Redact(env.DatabaseURL), "")
			}

			return pass(name)
		},
	}
}

// newReadOnlyLoginCheck asserts the least-privilege login exists and can connect, connecting the
// same way the service does.
func newReadOnlyLoginCheck() Check {
	return Check{
		Name:        "readonly login",
		Description: "the MCP query tools read through a login that can only read",
		Needs:       []string{"database"},
		Run: func(ctx context.Context, env Env) Result {
			if env.ReadOnlyDatabaseURL == "" {
				return fail(
					errReadOnlyLoginUnset,
					"",
					"set KOMRADE_READONLY_DATABASE_URL to the reader login komrade engine ensure made",
				)
			}

			if env.ReadOnlyDatabaseURL == env.DatabaseURL {
				return fail(
					errReadOnlyLoginShared,
					dsn.Redact(env.ReadOnlyDatabaseURL),
					"point KOMRADE_READONLY_DATABASE_URL at the reader login komrade engine ensure made",
				)
			}

			if err := env.ConnectReadOnly(ctx, env.ReadOnlyDatabaseURL); err != nil {
				return fail(
					fmt.Errorf("%w: %s", errReadOnlyLoginUnusable, describeDatabaseReason(err)),
					dsn.Redact(env.ReadOnlyDatabaseURL),
					"komrade engine ensure NAME, which resets the reader login's password to the engine's",
				)
			}

			if where := describeStoreMismatch(env.DatabaseURL, env.ReadOnlyDatabaseURL); where != "" {
				return warn(
					fmt.Errorf("%w: %s", errReadOnlyLoginElsewhere, where),
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
			return fail(fmt.Errorf("%w: %s", errDatabaseAuthFailed, pgErr.Message), detail,
				"check the user and password in the database url")
		case pgDatabaseMissing:
			return fail(fmt.Errorf("%w: %s", errDatabaseMissing, pgErr.Message), detail,
				"createdb "+readDatabaseName(url)+", or point the database url at one that exists")
		}
	}

	return fail(
		fmt.Errorf("%w %s: %s", errDatabaseUnreachable, describeHostPort(url), describeNetworkReason(err)),
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
