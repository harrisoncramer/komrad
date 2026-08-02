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

// fakeProbe answers for a database that is already open.
type fakeProbe struct {
	pending []string
	err     error
	closed  bool
}

func (f *fakeProbe) Pending(context.Context) ([]string, error) { return f.pending, f.err }
func (f *fakeProbe) Close()                                    { f.closed = true }

// dbEnv answers for a Postgres without one running. connectErr is what opening the writable
// login does, readOnlyErr what opening the least-privilege one does.
func dbEnv(probe *fakeProbe, connectErr, readOnlyErr error) Env {
	return Env{
		Settings: Settings{DatabaseURL: writableURL, ReadOnlyDatabaseURL: readOnlyURL},
		Connect: func(context.Context, string) (Probe, error) {
			if connectErr != nil {
				return nil, connectErr
			}

			return probe, nil
		},
		ConnectReadOnly: func(context.Context, string) error { return readOnlyErr },
	}
}

// refused is what a dial at an address with nothing listening comes back as, wrapped the
// way pgx wraps it.
func refused() error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
}

// TestDatabaseCheckTellsTheFailuresApart is the point of this check. Every one of these
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
			name: "a database that answers",
			url:  writableURL,
			want: StatusOK,
		},
		{
			name:       "nothing listening",
			url:        writableURL,
			connectErr: refused(),
			want:       StatusFail,
			wantCode:   "database_unreachable",
			wantFix:    "mise run docker:up",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := dbEnv(&fakeProbe{}, tt.connectErr, nil)
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

// TestDatabaseCheckHidesThePassword covers that a report is safe to paste. The address is
// the part worth reading and the password is not part of it.
func TestDatabaseCheckHidesThePassword(t *testing.T) {
	result := Database().Run(t.Context(), dbEnv(&fakeProbe{}, nil, nil))

	assert.NotContains(t, result.Detail, "weave:weave")
	assert.NotContains(t, result.Detail, "sslmode")
	assert.Equal(t, "postgres://weave@localhost:5460/weave", result.Detail)
}

// TestDatabaseFixNamesComposeOnlyForItsOwnPort covers that somebody running their own
// Postgres is not told to start a container they do not use.
func TestDatabaseFixNamesComposeOnlyForItsOwnPort(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "the port the checked-in compose file publishes",
			url:  writableURL,
			want: "mise run docker:up",
		},
		{
			name: "somebody else's Postgres on the standard port",
			url:  "postgres://weave:weave@localhost:5432/weave",
			want: "start the Postgres at that address, or point DATABASE_URL at one that is running",
		},
		{
			name: "a Postgres on another host",
			url:  "postgres://weave:weave@db.internal:5460/weave",
			want: "start the Postgres at that address, or point DATABASE_URL at one that is running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := dbEnv(&fakeProbe{}, refused(), nil)
			env.DatabaseURL = tt.url

			assert.Equal(t, tt.want, Database().Run(t.Context(), env).Fix)
		})
	}
}

// TestSchemaCheck covers that being behind is reported rather than treated as broken, since
// the service migrates on the way up.
func TestSchemaCheck(t *testing.T) {
	t.Run("up to date", func(t *testing.T) {
		result := Schema().Run(t.Context(), dbEnv(&fakeProbe{}, nil, nil))

		assert.Equal(t, StatusOK, result.Status)
		assert.Equal(t, "up to date", result.Detail)
	})

	t.Run("behind", func(t *testing.T) {
		probe := &fakeProbe{pending: []string{"20260727000000_initial.sql", "20260801000000_next.sql"}}
		result := Schema().Run(t.Context(), dbEnv(probe, nil, nil))

		assert.Equal(t, StatusWarn, result.Status, "the service applies these itself on the way up")
		assert.Equal(t, "schema_behind", CodeOf(result.Err))
		assert.Contains(t, result.Err.Error(), "2 migrations")
	})

	t.Run("one behind reads as one", func(t *testing.T) {
		probe := &fakeProbe{pending: []string{"20260727000000_initial.sql"}}
		result := Schema().Run(t.Context(), dbEnv(probe, nil, nil))

		assert.Contains(t, result.Err.Error(), "1 migration not")
	})

	t.Run("a ledger that will not read", func(t *testing.T) {
		probe := &fakeProbe{err: errors.New("permission denied for table schema_migrations")}
		result := Schema().Run(t.Context(), dbEnv(probe, nil, nil))

		assert.Equal(t, StatusFail, result.Status)
		assert.Equal(t, "schema_unreadable", CodeOf(result.Err))
	})
}

// TestSchemaCheckClosesWhatItOpened covers that a run of the whole list does not leave a
// pool per database check behind.
func TestSchemaCheckClosesWhatItOpened(t *testing.T) {
	probe := &fakeProbe{}
	Schema().Run(t.Context(), dbEnv(probe, nil, nil))

	assert.True(t, probe.closed)
}

// TestReadOnlyLoginCheck covers the login the service hard-requires. Without it serve exits
// before it listens, and the panel shows a startup error naming none of this.
func TestReadOnlyLoginCheck(t *testing.T) {
	t.Run("a login that works", func(t *testing.T) {
		result := ReadOnlyLogin().Run(t.Context(), dbEnv(&fakeProbe{}, nil, nil))

		assert.Equal(t, StatusOK, result.Status)
		assert.Equal(t, "postgres://weave_readonly@localhost:5460/weave", result.Detail)
	})

	t.Run("a login that does not exist yet", func(t *testing.T) {
		refusal := &pgconn.PgError{Code: "28P01", Message: `password authentication failed for user "weave_readonly"`}
		result := ReadOnlyLogin().Run(t.Context(), dbEnv(&fakeProbe{}, nil, refusal))

		assert.Equal(t, StatusFail, result.Status)
		assert.Equal(t, "readonly_login_unusable", CodeOf(result.Err))
		assert.Equal(t, "weave readonly-role", result.Fix)
		assert.Contains(t, result.Err.Error(), "weave_readonly",
			"the reason names the login, since that is what has to be created")
	})

	t.Run("one login doing both jobs", func(t *testing.T) {
		env := dbEnv(&fakeProbe{}, nil, nil)
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
