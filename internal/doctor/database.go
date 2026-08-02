package doctor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

// The ways the database can be wrong. They are separate codes rather than one because the
// fix for each is different, and a refused connection and a rejected password look the
// same from the panel: a server that would not start.
var (
	ErrDatabaseURLInvalid = &Problem{
		Code:    "database_url_invalid",
		Message: "DATABASE_URL is not a connection string Postgres accepts",
	}
	ErrDatabaseUnreachable = &Problem{
		Code:    "database_unreachable",
		Message: "nothing answered",
	}
	ErrDatabaseAuthFailed = &Problem{
		Code:    "database_auth_failed",
		Message: "the database refused the user or password in DATABASE_URL",
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
	ErrReadOnlyLoginUnusable = &Problem{
		Code:    "readonly_login_unusable",
		Message: "the least-privilege login the MCP tools connect as does not work",
	}
)

// Database asserts Postgres is there and will let weave in.
//
// It connects read-only rather than the way the service does, because opening the store
// applies the migrations, and a command run to find out what is wrong must not be the thing
// that changes it.
func Database() Check {
	return Check{
		Name:        "database",
		Description: "every agent, workflow, and schedule weave knows about lives in Postgres",
		Run: func(ctx context.Context, env Env) Result {
			// Parsed before connecting so a malformed string is its own answer. A
			// connection attempt would report it as a failure to reach an address
			// that was never an address.
			if _, err := pgconn.ParseConfig(env.DatabaseURL); err != nil {
				return fail(
					fmt.Errorf("%w: %w", ErrDatabaseURLInvalid, err),
					"",
					"set DATABASE_URL to a postgres:// url",
				)
			}

			target := redactDSN(env.DatabaseURL)

			handle, err := env.Connect(ctx, env.DatabaseURL)
			if err != nil {
				return databaseFailure(err, env.DatabaseURL, target)
			}
			defer handle.Close()

			return ok(target)
		},
	}
}

// Schema asserts the database has every migration this build ships.
//
// Being behind is not a failure the user has to fix by hand, since the service migrates on
// the way up, so this reports what is outstanding rather than treating it as broken.
func Schema() Check {
	return Check{
		Name:        "schema",
		Description: "the tables weave reads have to match the build reading them",
		Needs:       []string{"database"},
		Run: func(ctx context.Context, env Env) Result {
			handle, err := env.Connect(ctx, env.DatabaseURL)
			if err != nil {
				return databaseFailure(err, env.DatabaseURL, "")
			}
			defer handle.Close()

			pending, err := handle.Pending(ctx)
			if err != nil {
				return fail(
					fmt.Errorf("%w: %w", ErrSchemaUnreadable, err),
					"",
					"weave migrate",
				)
			}

			if len(pending) > 0 {
				return warn(
					fmt.Errorf("%w: %s not applied yet", ErrSchemaBehind, countMigrations(len(pending))),
					"",
				)
			}

			return ok("up to date")
		},
	}
}

// ReadOnlyLogin asserts the least-privilege login exists and can connect.
//
// This is the sharpest edge on a fresh machine. The service opens a second pool as this
// login and refuses to start when it cannot, so a user who has done everything else right
// still gets a panel that will not open, and the only thing that creates the login is a
// hidden subcommand.
//
// It connects exactly the way the service does, superuser refusal included, so passing here
// means the service gets past the same call rather than something close to it.
func ReadOnlyLogin() Check {
	return Check{
		Name:        "readonly login",
		Description: "the MCP query tools read through a login that can only read",
		Needs:       []string{"database"},
		Run: func(ctx context.Context, env Env) Result {
			// Checked before connecting, since one login for both would connect
			// cleanly and hand the MCP endpoint the writable role.
			if env.ReadOnlyDatabaseURL == env.DatabaseURL {
				return fail(
					ErrReadOnlyLoginShared,
					redactDSN(env.ReadOnlyDatabaseURL),
					"point WEAVE_READONLY_DATABASE_URL at its own login, then run weave readonly-role",
				)
			}

			if err := env.ConnectReadOnly(ctx, env.ReadOnlyDatabaseURL); err != nil {
				return fail(
					fmt.Errorf("%w: %s", ErrReadOnlyLoginUnusable, shortDatabaseReason(err)),
					redactDSN(env.ReadOnlyDatabaseURL),
					"weave readonly-role",
				)
			}

			return ok(redactDSN(env.ReadOnlyDatabaseURL))
		},
	}
}

// databaseFailure names what Postgres actually objected to, and what to do about it.
//
// Every one of these arrives at the panel as the same startup failure, so telling them
// apart is most of what this check is worth: a server that is not running, a password that
// is wrong, and a database that was never created need three different things done.
func databaseFailure(err error, url, detail string) Result {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgAuthFailed, pgAuthUnspecified:
			return fail(fmt.Errorf("%w: %s", ErrDatabaseAuthFailed, pgErr.Message), detail,
				"check the user and password in DATABASE_URL")
		case pgDatabaseMissing:
			return fail(fmt.Errorf("%w: %s", ErrDatabaseMissing, pgErr.Message), detail,
				"create it, or point DATABASE_URL at one that exists")
		}
	}

	return fail(
		fmt.Errorf("%w %s: %s", ErrDatabaseUnreachable, hostPort(url), networkReason(err)),
		detail, startPostgresFix(url))
}

// shortDatabaseReason is what went wrong in the words Postgres or the network used, without
// the connection attempt spelled out around them.
func shortDatabaseReason(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Message
	}

	return networkReason(err)
}

// networkReason is why the dial did not land, in the few words that say it.
//
// pgx reports the whole attempt, which for a host that resolves to both an IPv4 and an
// IPv6 address is two dial errors and the connection string spelled out again. All of that
// is one fact, and the row it has to fit on is one line.
func networkReason(err error) string {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		return opErr.Err.Error()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}

	return err.Error()
}

// hostPort is where the connection was attempted, for a message that has to name it since
// the reason alone does not.
func hostPort(url string) string {
	cfg, err := pgconn.ParseConfig(url)
	if err != nil {
		return "the configured address"
	}

	return "at " + cfg.Host + ":" + strconv.Itoa(int(cfg.Port))
}

// The SQLSTATEs worth telling apart. Postgres reports each as a connection failure, and
// only the code says which one it was.
const (
	pgAuthFailed      = "28P01"
	pgAuthUnspecified = "28000"
	pgDatabaseMissing = "3D000"
)

// startPostgresFix is what to run when nothing answered. The local compose Postgres is
// named only when the address is the one it listens on, since telling someone with their
// own Postgres to start a container is worse than saying nothing.
func startPostgresFix(url string) string {
	if isLocalCompose(url) {
		return "mise run docker:up"
	}

	return "start the Postgres at that address, or point DATABASE_URL at one that is running"
}

// isLocalCompose reports whether the url is the port the checked-in compose file publishes,
// which is the only address weave can say anything specific about.
func isLocalCompose(url string) bool {
	cfg, err := pgconn.ParseConfig(url)
	if err != nil {
		return false
	}

	return cfg.Port == composePort && (cfg.Host == "localhost" || cfg.Host == "127.0.0.1")
}

// composePort is the port docker-compose.yml publishes Postgres on by default.
const composePort = 5460

// redactDSN is the connection string with the password taken out, since a report is pasted
// into issues and the address is the part worth reading anyway.
func redactDSN(url string) string {
	cfg, err := pgconn.ParseConfig(url)
	if err != nil {
		return "(unparseable)"
	}

	return "postgres://" + cfg.User + "@" + cfg.Host + ":" + strconv.Itoa(int(cfg.Port)) + "/" + cfg.Database
}

// countMigrations writes a number of migrations as a phrase, so the line reads as a
// sentence rather than as a count with a noun stuck to it.
func countMigrations(n int) string {
	if n == 1 {
		return "1 migration"
	}

	return strconv.Itoa(n) + " migrations"
}
