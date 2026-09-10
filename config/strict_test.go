package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadStrict writes one config file and returns what Load made of it, with the connection
// environment cleared so the file is what is being read rather than the pane weave started.
func loadStrict(t *testing.T, file string) (*Config, error) {
	t.Helper()

	clearConnectionEnvironment(t)

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(file), 0o600))

	return Load([]string{path})
}

// TestLoadRejectsMailTheServiceCouldNotSendOn covers the two mail settings the service reads at
// send time, where a value it cannot read stops every send rather than the config being read.
func TestLoadRejectsMailTheServiceCouldNotSendOn(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{
			name: "a deadline in words rather than a duration",
			file: "[mail]\ndelivery_deadline = \"3 hours\"\n",
			want: "mail.delivery_deadline",
		},
		{
			name: "a deadline that has already passed",
			file: "[mail]\ndelivery_deadline = \"-5m\"\n",
			want: "mail.delivery_deadline",
		},
		{
			name: "an exchange limit below none",
			file: "[mail]\nmax_exchange = -3\n",
			want: "mail.max_exchange",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadStrict(t, tt.file)

			require.Error(t, err, "the service reads this per send, so a bad value stops mail rather than startup")
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestLoadRejectsAnAddressTheApiShouldNotAnswerOn covers the address the service listens on,
// which has no auth and whose writes spawn agents.
func TestLoadRejectsAnAddressTheApiShouldNotAnswerOn(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "a word rather than a host and port",
			file: "[connection]\naddr = \"nonsense\"\n",
		},
		{
			name: "a port that is not a number",
			file: "[connection]\naddr = \"127.0.0.1:http\"\n",
		},
		{
			name: "every interface, written as no host at all",
			file: "[connection]\naddr = \":8787\"\n",
		},
		{
			name: "every interface, written out",
			file: "[connection]\naddr = \"0.0.0.0:8787\"\n",
		},
		{
			name: "a host on the network",
			file: "[connection]\naddr = \"192.168.1.20:8787\"\n",
		},
		{
			name: "a name rather than an address",
			file: "[connection]\naddr = \"my-laptop.local:8787\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadStrict(t, tt.file)

			require.Error(t, err, "the api has no auth, so it answers on this machine only")
			assert.Contains(t, err.Error(), "connection.addr")
		})
	}
}

// TestLoadRejectsAPortOutsideTheRange covers the numbers Atoi takes and a listener does not,
// which is the class the port check exists for.
func TestLoadRejectsAPortOutsideTheRange(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:99999", "127.0.0.1:-1", "127.0.0.1:0"} {
		t.Run(addr, func(t *testing.T) {
			_, err := loadStrict(t, "[connection]\naddr = \""+addr+"\"\n")

			require.Error(t, err, "a listener refuses this, so the file should too")
			assert.Contains(t, err.Error(), "connection.addr")
		})
	}
}

// TestLoadTakesTheDefaultForAnEmptyConnection covers the settings an empty value used to survive
// as, where an empty address listens on every interface on a port nothing else can predict.
func TestLoadTakesTheDefaultForAnEmptyConnection(t *testing.T) {
	cfg, err := loadStrict(t, "[connection]\naddr = \"\"\ndatabase_url = \"\"\ntmux_socket = \"\"\n")

	require.NoError(t, err, "an empty value is how an unset setting reads, so it takes the default")
	assert.Equal(t, DefaultAddr, cfg.Connection.Addr)
	assert.Equal(t, DefaultDatabaseURL, cfg.Connection.DatabaseURL)
	assert.Equal(t, DefaultTmuxSocket, cfg.Connection.TmuxSocket)
}

// TestValidateAddrRefusesNothing covers the address arriving as a flag rather than a setting,
// where there is no default underneath it.
func TestValidateAddrRefusesNothing(t *testing.T) {
	require.Error(t, ValidateAddr(""), "an empty listen address answers on every interface")
	require.Error(t, ValidateAddr("0.0.0.0:8787"))
	require.NoError(t, ValidateAddr("127.0.0.1:8787"))
}

// TestLoadTakesEveryLoopbackAddress covers the spellings that reach only this machine, since a
// rule that refused one of them would take the panel off the air.
func TestLoadTakesEveryLoopbackAddress(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8787", "127.0.0.2:8790", "localhost:8787", "[::1]:8787"} {
		t.Run(addr, func(t *testing.T) {
			cfg, err := loadStrict(t, "[connection]\naddr = \""+addr+"\"\n")

			require.NoError(t, err)
			assert.Equal(t, addr, cfg.Connection.Addr)
		})
	}
}

// TestLoadRejectsADatabaseUrlPostgresWouldNotAnswer covers the url the read-only login is derived
// from, which comes out empty for anything else and fails a check that names something else.
func TestLoadRejectsADatabaseUrlPostgresWouldNotAnswer(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "another database's scheme",
			file: "[connection]\ndatabase_url = \"mysql://root@localhost/weave\"\n",
		},
		{
			name: "a string that is not a url",
			file: "[connection]\ndatabase_url = \"::::\"\n",
		},
		{
			name: "a path with no scheme",
			file: "[connection]\ndatabase_url = \"/var/run/postgres\"\n",
		},
		{
			name: "a postgres url naming no server",
			file: "[connection]\ndatabase_url = \"postgres://\"\n",
		},
		{
			name: "a postgres url that is only a scheme",
			file: "[connection]\ndatabase_url = \"postgres:\"\n",
		},
		{
			name: "a postgres url carrying only a query",
			file: "[connection]\ndatabase_url = \"postgres://?sslmode=disable\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadStrict(t, tt.file)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "connection.database_url")
		})
	}
}

// TestLoadTakesEitherPostgresScheme covers both spellings Postgres answers to, since the
// read-only login is derived from either.
func TestLoadTakesEitherPostgresScheme(t *testing.T) {
	for _, databaseURL := range []string{
		"postgres://weave:weave@localhost:5460/weave?sslmode=disable",
		"postgresql://weave@localhost/weave",
	} {
		t.Run(databaseURL, func(t *testing.T) {
			cfg, err := loadStrict(t, "[connection]\ndatabase_url = \""+databaseURL+"\"\n")

			require.NoError(t, err)
			assert.NotEmpty(t, cfg.Connection.ReadOnlyDatabaseURL, "a read-only login is derived from it")
		})
	}
}
