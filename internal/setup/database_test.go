package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"komrad/config"
	"komrad/internal/doctor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatabaseStep covers when the database step is done, asks, or stops.
func TestDatabaseStep(t *testing.T) {
	clearConnectionEnv(t)

	tests := []struct {
		name     string
		config   string
		findings []doctor.Finding
		verdict  verdict
	}{
		{
			name:     "nothing answers at the default url",
			findings: []doctor.Finding{ok(doctor.CheckConfig, ""), failed(doctor.CheckDatabase)},
			verdict:  verdictAsk,
		},
		{
			name: "a Postgres answering at the default url is not one the person chose",
			findings: []doctor.Finding{
				ok(doctor.CheckConfig, ""),
				ok(doctor.CheckDatabase, ""),
				failed(doctor.CheckSchema),
			},
			verdict: verdictAsk,
		},
		{
			name:     "a broken config stops",
			findings: []doctor.Finding{failed(doctor.CheckConfig), failed(doctor.CheckDatabase)},
			verdict:  verdictStop,
		},
		{
			name:     "a config file the person has that names no database stops",
			config:   "# mine\n",
			findings: []doctor.Finding{ok(doctor.CheckConfig, ""), failed(doctor.CheckDatabase)},
			verdict:  verdictStop,
		},
		{
			name:     "a url the person set that nothing answers is theirs to fix",
			config:   "[connection]\ndatabase_url = \"postgres://komrad@db.example.com/komrad\"\n",
			findings: []doctor.Finding{ok(doctor.CheckConfig, ""), failed(doctor.CheckDatabase)},
			verdict:  verdictStop,
		},
		{
			name:   "the embedded Postgres being down stops",
			config: "[connection]\nembedded_postgres = true\n",
			findings: []doctor.Finding{
				ok(doctor.CheckConfig, ""),
				failed(doctor.CheckEmbeddedPostgres),
				failed(doctor.CheckDatabase),
			},
			verdict: verdictStop,
		},
		{
			name:   "a schema behind the build stops",
			config: "[connection]\ndatabase_url = \"postgres://komrad@db.example.com/komrad\"\n",
			findings: []doctor.Finding{
				ok(doctor.CheckConfig, ""),
				ok(doctor.CheckDatabase, ""),
				failed(doctor.CheckSchema),
			},
			verdict: verdictStop,
		},
		{
			name:   "an engine in the embedded Postgres",
			config: "[connection]\nembedded_postgres = true\ndatabase_url = \"postgres://komrad_engine_default:pw@/komrad?host=%2Fstate%2Fpostgres\"\n",
			findings: []doctor.Finding{
				ok(doctor.CheckConfig, ""),
				ok(doctor.CheckEmbeddedPostgres, ""),
				ok(doctor.CheckDatabase, ""),
				ok(doctor.CheckSchema, ""),
				ok(doctor.CheckEngine, "default"),
			},
			verdict: verdictDone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := homeConfig(t)
			if tt.config != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				require.NoError(t, os.WriteFile(path, []byte(tt.config), 0o600))
			}
			step := &DatabaseStep{File: &ConfigFile{Path: path}, Stack: config.Stack{}}

			observation := step.observe(t.Context(), fakeChecker(tt.findings...))

			assert.Equal(t, tt.verdict, observation.Verdict)
		})
	}
}

// TestDatabaseStepUnattendedNamesTheFlags covers that a run with nobody to ask names only the flags this build can act on.
func TestDatabaseStepUnattendedNamesTheFlags(t *testing.T) {
	tests := []struct {
		name                string
		hasEmbeddedPostgres bool
		names               []string
		omits               []string
	}{
		{
			name:  "a build carrying no embedded Postgres",
			names: []string{"--owner-database-url"},
			omits: []string{"--embedded-postgres"},
		},
		{
			name:                "a build carrying the embedded Postgres",
			hasEmbeddedPostgres: true,
			names:               []string{"--embedded-postgres", "--owner-database-url"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&DatabaseStep{HasEmbeddedPostgres: tt.hasEmbeddedPostgres}).answerUnattended()

			require.ErrorIs(t, err, errNeedsAnswer)
			for _, flag := range tt.names {
				require.ErrorContains(t, err, flag)
			}
			for _, flag := range tt.omits {
				assert.NotContains(t, err.Error(), flag)
			}
		})
	}
}

// TestDatabaseStepRepair covers the config each answer writes, and the embedded Postgres removed when creating the engine fails.
func TestDatabaseStepRepair(t *testing.T) {
	clearConnectionEnv(t)

	const (
		ownerURL  = "postgres://komrad:secret@/komrad?host=%2Fstate%2Fpostgres"
		engineURL = "postgres://komrad_engine_default:pw@/komrad?host=%2Fstate%2Fpostgres"
	)

	tests := []struct {
		name             string
		embeddedPostgres bool
		ownerURLFlag     string
		createEngineErr  error
		removed          bool
		after            string
	}{
		{
			name:             "the embedded Postgres is removed when creating the engine fails",
			embeddedPostgres: true,
			createEngineErr:  errors.New("migrate refused"),
			removed:          true,
		},
		{
			name:            "a Postgres the person runs is left alone when creating the engine fails",
			ownerURLFlag:    ownerURL,
			createEngineErr: errors.New("migrate refused"),
		},
		{
			name:             "embedded Postgres",
			embeddedPostgres: true,
			after:            "[connection]\nembedded_postgres = true\nowner_database_url = \"" + ownerURL + "\"\ndatabase_url = \"" + engineURL + "\"\n",
		},
		{
			name:         "a Postgres the person runs",
			ownerURLFlag: " " + ownerURL + "\n",
			after:        "[connection]\nowner_database_url = \"" + ownerURL + "\"\ndatabase_url = \"" + engineURL + "\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := homeConfig(t)
			var migratedOwnerURL string
			var removed bool
			step := &DatabaseStep{
				File:                   &ConfigFile{Path: path},
				Stack:                  config.Stack{},
				EmbeddedPostgresFlag:   tt.embeddedPostgres,
				OwnerURLFlag:           tt.ownerURLFlag,
				CreateEmbeddedPostgres: func(context.Context) (string, error) { return ownerURL, nil },
				RemoveEmbeddedPostgres: func(context.Context) error {
					removed = true

					return nil
				},
				MigrateAndCreateEngine: func(_ context.Context, owner string) (string, error) {
					migratedOwnerURL = owner

					return engineURL, tt.createEngineErr
				},
			}
			require.NoError(t, step.answerUnattended())

			err := step.repair(t.Context())

			if tt.createEngineErr != nil {
				require.ErrorIs(t, err, tt.createEngineErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, ownerURL, migratedOwnerURL)
			assert.Equal(t, tt.removed, removed)
			if tt.after == "" {
				assert.NoFileExists(t, path)

				return
			}
			written, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.after, string(written))
		})
	}
}

func clearConnectionEnv(t *testing.T) {
	t.Helper()

	for _, variable := range []string{"KOMRAD_DATABASE_URL", "KOMRAD_OWNER_DATABASE_URL", "KOMRAD_EMBEDDED_POSTGRES"} {
		t.Setenv(variable, "")
	}
}

// homeConfig points the home directory at a temp dir and returns the config file komrad reads there when no --config is given.
func homeConfig(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	return filepath.Join(home, ".config", "komrad", "config.toml")
}

func failed(name string) doctor.Finding {
	return doctor.Finding{
		Name:   name,
		Status: doctor.StatusFail,
		Error:  name + " failed",
	}
}
