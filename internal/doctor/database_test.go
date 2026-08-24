package doctor

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	writableURL = "postgres://weave:weave@localhost:5460/weave?sslmode=disable"
	readOnlyURL = "postgres://weave_readonly:weave_readonly@localhost:5460/weave?sslmode=disable"
)

type fakeSchema struct {
	pending []string
	err     error
}

func dbEnv(schema fakeSchema, connectErr, readOnlyErr error) Env {
	return Env{
		Settings:     Settings{DatabaseURL: writableURL, ReadOnlyDatabaseURL: readOnlyURL},
		PingDatabase: func(context.Context, string) error { return connectErr },
		ReadPendingMigrations: func(context.Context, string) ([]string, error) {
			if connectErr != nil {
				return nil, connectErr
			}

			return schema.pending, schema.err
		},
		ConnectReadOnly: func(context.Context, string) error { return readOnlyErr },
	}
}

func refused() error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
}

// TestDatabaseCheckTellsTheFailuresApart is the point of this check: every one of these
// reaches the panel as the same startup error, and each needs a different thing done.
func TestDatabaseCheckTellsTheFailuresApart(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		connectErr error
		want       Status
		wantCode   string
		wantFix    string
	}{
		{
			name:       "nothing listening",
			url:        writableURL,
			connectErr: refused(),
			want:       StatusFail,
			wantCode:   "database_unreachable",
			wantFix:    "start Postgres at localhost:5460, or point the database url at one that is running",
		},
		{
			name:       "a password the server rejected",
			url:        writableURL,
			connectErr: &pgconn.PgError{Code: "28P01", Message: `password authentication failed for user "weave"`},
			want:       StatusFail,
			wantCode:   "database_auth_failed",
		},
		{
			name:       "a server that is up with no such database",
			url:        writableURL,
			connectErr: &pgconn.PgError{Code: "3D000", Message: `database "weave" does not exist`},
			want:       StatusFail,
			wantCode:   "database_missing",
		},
		{
			name:     "a url that is not one",
			url:      "not-a-url",
			want:     StatusFail,
			wantCode: "database_url_invalid",
		},
		{
			name: "a database that answers",
			url:  writableURL,
			want: StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := dbEnv(fakeSchema{}, tt.connectErr, nil)
			env.DatabaseURL = tt.url

			result := Database().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, CodeOf(result.Err))
			if tt.wantFix != "" {
				assert.Equal(t, tt.wantFix, result.Fix)
			}
		})
	}
}

// TestDatabaseCheckHidesThePassword covers that a report is safe to paste: the address is
// the part worth reading and the password is not part of it.
func TestDatabaseCheckHidesThePassword(t *testing.T) {
	result := Database().Run(t.Context(), dbEnv(fakeSchema{}, nil, nil))

	assert.NotContains(t, result.Detail, "weave:weave")
	assert.NotContains(t, result.Detail, "sslmode")
	assert.Equal(t, "postgres://weave@localhost:5460/weave", result.Detail)
}

// TestDatabaseFixAssumesNoCheckout covers that a fix is something the reader can actually
// run: most people who hit this installed a binary, so they have no checkout, no mise, and
// no compose file, and the fix names the address it could not reach rather than guessing how
// that Postgres is meant to start.
func TestDatabaseFixAssumesNoCheckout(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "the address the checked-in compose file happens to use",
			url:  writableURL,
			want: "start Postgres at localhost:5460, or point the database url at one that is running",
		},
		{
			name: "somebody else's Postgres on the standard port",
			url:  "postgres://weave:weave@localhost:5432/weave",
			want: "start Postgres at localhost:5432, or point the database url at one that is running",
		},
		{
			name: "a Postgres on another host",
			url:  "postgres://weave:weave@db.internal:5460/weave",
			want: "start Postgres at db.internal:5460, or point the database url at one that is running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := dbEnv(fakeSchema{}, refused(), nil)
			env.DatabaseURL = tt.url

			assert.Equal(t, tt.want, Database().Run(t.Context(), env).Fix)
			assert.NotContains(t, Database().Run(t.Context(), env).Fix, "mise",
				"a fix never names a command that only exists inside a checkout")
		})
	}
}

// TestDatabaseMissingFixNamesTheDatabase covers that the fix is runnable rather than a
// description of the goal.
func TestDatabaseMissingFixNamesTheDatabase(t *testing.T) {
	missing := &pgconn.PgError{Code: "3D000", Message: `database "weave" does not exist`}
	result := Database().Run(t.Context(), dbEnv(fakeSchema{}, missing, nil))

	assert.Equal(t, "createdb weave, or point the database url at one that exists", result.Fix)
}

// TestSchemaCheck covers that being behind is reported rather than treated as broken, since
// the service migrates on the way up.
func TestSchemaCheck(t *testing.T) {
	t.Run("up to date", func(t *testing.T) {
		result := Schema().Run(t.Context(), dbEnv(fakeSchema{}, nil, nil))

		assert.Equal(t, StatusOK, result.Status)
		assert.Equal(t, "up to date", result.Detail)
	})

	t.Run("behind", func(t *testing.T) {
		schema := fakeSchema{pending: []string{"20260727000000_initial.sql", "20260801000000_next.sql"}}
		result := Schema().Run(t.Context(), dbEnv(schema, nil, nil))

		assert.Equal(t, StatusWarn, result.Status, "the service applies these itself on the way up")
		assert.Equal(t, "schema_behind", CodeOf(result.Err))
		assert.Contains(t, result.Err.Error(), "2 migrations")
	})

	t.Run("one behind reads as one", func(t *testing.T) {
		schema := fakeSchema{pending: []string{"20260727000000_initial.sql"}}
		result := Schema().Run(t.Context(), dbEnv(schema, nil, nil))

		assert.Contains(t, result.Err.Error(), "1 migration not")
	})

	t.Run("a ledger that will not read", func(t *testing.T) {
		schema := fakeSchema{err: errors.New("permission denied for table schema_migrations")}
		result := Schema().Run(t.Context(), dbEnv(schema, nil, nil))

		assert.Equal(t, StatusFail, result.Status)
		assert.Equal(t, "schema_unreadable", CodeOf(result.Err))
	})
}

// TestReadOnlyLoginCheck covers the login the service hard-requires: without it serve exits
// before it listens, and the panel shows a startup error naming none of this.
func TestReadOnlyLoginCheck(t *testing.T) {
	t.Run("a login that works", func(t *testing.T) {
		result := ReadOnlyLogin().Run(t.Context(), dbEnv(fakeSchema{}, nil, nil))

		assert.Equal(t, StatusOK, result.Status)
		assert.Equal(t, "postgres://weave_readonly@localhost:5460/weave", result.Detail)
	})

	t.Run("a login that does not exist yet", func(t *testing.T) {
		refusal := &pgconn.PgError{Code: "28P01", Message: `password authentication failed for user "weave_readonly"`}
		result := ReadOnlyLogin().Run(t.Context(), dbEnv(fakeSchema{}, nil, refusal))

		assert.Equal(t, StatusFail, result.Status)
		assert.Equal(t, "readonly_login_unusable", CodeOf(result.Err))
		assert.Equal(t, "weave readonly-role", result.Fix)
		assert.Contains(t, result.Err.Error(), "weave_readonly",
			"the reason names the login, since that is what has to be created")
	})

	t.Run("a login on another server", func(t *testing.T) {
		env := dbEnv(fakeSchema{}, nil, nil)
		env.ReadOnlyDatabaseURL = "postgres://weave_readonly:weave_readonly@localhost:5461/weave?sslmode=disable"

		result := ReadOnlyLogin().Run(t.Context(), env)

		require.Equal(t, StatusWarn, result.Status,
			"both logins connect, so nothing else in the report says the MCP tools are reading somewhere else")
		assert.Equal(t, "readonly_login_elsewhere", CodeOf(result.Err))
		assert.Contains(t, result.Err.Error(), "localhost:5460",
			"the reason names the address the panel writes to, since that is the one the pair should agree on")
	})

	t.Run("no read-only login to connect as", func(t *testing.T) {
		env := dbEnv(fakeSchema{}, nil, nil)
		env.ReadOnlyDatabaseURL = ""

		result := ReadOnlyLogin().Run(t.Context(), env)

		require.Equal(t, StatusFail, result.Status,
			"an empty url connects as whoever is running weave, so it cannot be handed to the pool")
		assert.Equal(t, "readonly_login_unset", CodeOf(result.Err))
	})

	t.Run("one login doing both jobs", func(t *testing.T) {
		env := dbEnv(fakeSchema{}, nil, nil)
		env.ReadOnlyDatabaseURL = env.DatabaseURL

		result := ReadOnlyLogin().Run(t.Context(), env)

		require.Equal(t, StatusFail, result.Status,
			"this connects cleanly and hands the MCP endpoint the writable role, so it has to be caught by name")
		assert.Equal(t, "readonly_login_shared", CodeOf(result.Err))
	})
}

// TestDatabaseChecksWaitOnTheDatabase covers that a Postgres nobody can reach is reported
// once, rather than as three failures that are all the same failure.
func TestDatabaseChecksWaitOnTheDatabase(t *testing.T) {
	for _, name := range []string{"schema", "readonly login"} {
		var found bool
		for _, check := range Checks(Settings{}) {
			if check.Name == name {
				assert.Contains(t, check.Needs, "database", "%s waits on the database", name)
				found = true
			}
		}
		require.True(t, found, "there is a check named %q", name)
	}
}
