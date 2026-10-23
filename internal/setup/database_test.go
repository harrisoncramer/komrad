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

// TestDatabaseStep covers when the database step is done, repairs alone, asks, or stops.
func TestDatabaseStep(t *testing.T) {
	clearConnectionEnv(t)

	tests := []struct {
		name                 string
		config               string
		noEmbeddedPostgres   bool
		noEmbeddedInTheBuild bool
		ownerURLFlag         string
		findings             []doctor.Finding
		verdict              verdict
	}{
		{
			name: "no embedded Postgres is set up",
			findings: []doctor.Finding{
				ok(doctor.CheckConfig, ""),
				failed(doctor.CheckEmbeddedPostgres),
				failed(doctor.CheckDatabase),
			},
			verdict: verdictRepair,
		},
		{
			name: "an embedded Postgres with no engine",
			findings: []doctor.Finding{
				ok(doctor.CheckConfig, ""),
				ok(doctor.CheckEmbeddedPostgres, ""),
				ok(doctor.CheckDatabase, ""),
				ok(doctor.CheckSchema, ""),
				failed(doctor.CheckEngine),
			},
			verdict: verdictRepair,
		},
		{
			name: "an engine in the embedded Postgres",
			findings: []doctor.Finding{
				ok(doctor.CheckConfig, ""),
				ok(doctor.CheckEmbeddedPostgres, ""),
				ok(doctor.CheckDatabase, ""),
				ok(doctor.CheckSchema, ""),
				ok(doctor.CheckEngine, "default"),
			},
			verdict: verdictDone,
		},
		{
			name:               "turning the embedded Postgres off asks for the owner",
			noEmbeddedPostgres: true,
			findings:           []doctor.Finding{ok(doctor.CheckConfig, ""), failed(doctor.CheckEmbeddedPostgres)},
			verdict:            verdictAsk,
		},
		{
			name:         "an owner url passed takes it",
			ownerURLFlag: "postgres://komrad@db.example.com/komrad",
			findings:     []doctor.Finding{ok(doctor.CheckConfig, ""), failed(doctor.CheckEmbeddedPostgres)},
			verdict:      verdictAsk,
		},
		{
			name:                 "a build carrying no embedded Postgres asks for the owner",
			noEmbeddedInTheBuild: true,
			findings:             []doctor.Finding{ok(doctor.CheckConfig, ""), failed(doctor.CheckEmbeddedPostgres)},
			verdict:              verdictAsk,
		},
		{
			name:               "a config file the person has stops before taking an owner url",
			config:             "# mine\n",
			noEmbeddedPostgres: true,
			findings:           []doctor.Finding{ok(doctor.CheckConfig, ""), failed(doctor.CheckEmbeddedPostgres)},
			verdict:            verdictStop,
		},
		{
			name:     "a broken config stops",
			findings: []doctor.Finding{failed(doctor.CheckConfig), failed(doctor.CheckDatabase)},
			verdict:  verdictStop,
		},
		{
			name:     "a url the person set that nothing answers is theirs to fix",
			config:   "[connection]\ndatabase_url = \"postgres://komrad@db.example.com/komrad\"\n",
			findings: []doctor.Finding{ok(doctor.CheckConfig, ""), failed(doctor.CheckDatabase)},
			verdict:  verdictStop,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := homeConfig(t)
			if tt.config != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				require.NoError(t, os.WriteFile(path, []byte(tt.config), 0o600))
			}
			step := &DatabaseStep{
				File:                   &ConfigFile{Path: path},
				Stack:                  config.Stack{},
				HasEmbeddedPostgres:    !tt.noEmbeddedInTheBuild,
				NoEmbeddedPostgresFlag: tt.noEmbeddedPostgres,
				OwnerURLFlag:           tt.ownerURLFlag,
			}

			observation := step.observe(t.Context(), fakeChecker(tt.findings...))

			assert.Equal(t, tt.verdict, observation.Verdict)
		})
	}
}

// TestDatabaseStepUnattended covers taking the owner's url from its flag, and naming the flag when it is missing.
func TestDatabaseStepUnattended(t *testing.T) {
	tests := []struct {
		name                string
		hasEmbeddedPostgres bool
		ownerURLFlag        string
		wantErr             []string
	}{
		{
			name:    "a build carrying no embedded Postgres",
			wantErr: []string{"carries no embedded Postgres", "--owner-database-url"},
		},
		{
			name:                "the embedded Postgres turned off",
			hasEmbeddedPostgres: true,
			wantErr:             []string{"--owner-database-url"},
		},
		{
			name:                "an owner url passed",
			hasEmbeddedPostgres: true,
			ownerURLFlag:        "postgres://owner",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &DatabaseStep{
				HasEmbeddedPostgres:    tt.hasEmbeddedPostgres,
				NoEmbeddedPostgresFlag: true,
				OwnerURLFlag:           tt.ownerURLFlag,
			}

			err := step.answerUnattended()

			if len(tt.wantErr) == 0 {
				require.NoError(t, err)
				assert.Equal(t, tt.ownerURLFlag, step.pasted)

				return
			}
			require.ErrorIs(t, err, errNeedsAnswer)
			for _, text := range tt.wantErr {
				require.ErrorContains(t, err, text)
			}
		})
	}
}

// TestDatabaseStepRepair covers the config each path writes, and the embedded Postgres removed when this
// run created it and creating the engine fails.
func TestDatabaseStepRepair(t *testing.T) {
	clearConnectionEnv(t)
	t.Setenv("KOMRAD_STATE_DIR", "/state")

	const (
		embeddedOwnerURL = "postgres://komrad@/komrad?host=%2Fstate%2Fpostgres&port=5432"
		ownerURL         = "postgres://komrad:secret@db.example.com/komrad"
		engineURL        = "postgres://komrad_engine_default:pw@db.example.com/komrad"
	)

	tests := []struct {
		name            string
		ownerURLFlag    string
		existing        bool
		createEngineErr error
		wantOwnerURL    string
		removed         bool
		after           string
	}{
		{
			name:         "the embedded Postgres",
			wantOwnerURL: embeddedOwnerURL,
		},
		{
			name:            "the embedded Postgres this run created is removed when creating the engine fails",
			createEngineErr: errors.New("migrate refused"),
			wantOwnerURL:    embeddedOwnerURL,
			removed:         true,
		},
		{
			name:            "an embedded Postgres that was already there is kept when creating the engine fails",
			existing:        true,
			createEngineErr: errors.New("migrate refused"),
			wantOwnerURL:    embeddedOwnerURL,
		},
		{
			name:         "a Postgres the person runs",
			ownerURLFlag: " " + ownerURL + "\n",
			wantOwnerURL: ownerURL,
			after:        "[connection]\nowner_database_url = \"" + ownerURL + "\"\ndatabase_url = \"" + engineURL + "\"\n",
		},
		{
			name:            "a Postgres the person runs is left alone when creating the engine fails",
			ownerURLFlag:    ownerURL,
			createEngineErr: errors.New("migrate refused"),
			wantOwnerURL:    ownerURL,
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
				HasEmbeddedPostgres:    true,
				OwnerURLFlag:           tt.ownerURLFlag,
				CreateEmbeddedPostgres: func(context.Context) (bool, error) { return !tt.existing, nil },
				RemoveEmbeddedPostgres: func(context.Context) error {
					removed = true

					return nil
				},
				MigrateAndCreateEngine: func(_ context.Context, owner string) (string, error) {
					migratedOwnerURL = owner

					return engineURL, tt.createEngineErr
				},
			}
			if tt.ownerURLFlag != "" {
				require.NoError(t, step.answerUnattended())
			}

			err := step.repair(t.Context())

			if tt.createEngineErr != nil {
				require.ErrorIs(t, err, tt.createEngineErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantOwnerURL, migratedOwnerURL)
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

	for _, variable := range []string{"KOMRAD_DATABASE_URL", "KOMRAD_OWNER_DATABASE_URL", "KOMRAD_STATE_DIR"} {
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
