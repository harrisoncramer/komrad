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

func clearConnectionEnv(t *testing.T) {
	t.Helper()

	for _, variable := range []string{"KOMRAD_DATABASE_URL", "KOMRAD_OWNER_DATABASE_URL", "KOMRAD_RUN_POSTGRES"} {
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
			name:   "the Postgres komrad runs being down stops",
			config: "[connection]\nrun_postgres = true\n",
			findings: []doctor.Finding{
				ok(doctor.CheckConfig, ""),
				failed(doctor.CheckPostgres),
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
			name:   "an engine in the Postgres komrad runs",
			config: "[connection]\nrun_postgres = true\ndatabase_url = \"postgres://komrad_engine_laptop:pw@/komrad?host=%2Fstate%2Fpostgres\"\n",
			findings: []doctor.Finding{
				ok(doctor.CheckConfig, ""),
				ok(doctor.CheckPostgres, ""),
				ok(doctor.CheckDatabase, ""),
				ok(doctor.CheckSchema, ""),
				ok(doctor.CheckEngine, "laptop"),
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
	err := (&DatabaseStep{Bundled: true}).answerUnattended()
	require.ErrorIs(t, err, errNeedsAnswer)
	require.ErrorContains(t, err, "--run-postgres")

	err = (&DatabaseStep{}).answerUnattended()
	require.ErrorIs(t, err, errNeedsAnswer)
	assert.NotContains(t, err.Error(), "--run-postgres")
	assert.ErrorContains(t, err, "--owner-database-url")
}

// TestDatabaseStepRepair covers the config each answer writes, and the Postgres it made removed when joining fails.
func TestDatabaseStepRepair(t *testing.T) {
	clearConnectionEnv(t)

	const (
		ownerURL  = "postgres://komrad:secret@/komrad?host=%2Fstate%2Fpostgres"
		engineURL = "postgres://komrad_engine_laptop:pw@/komrad?host=%2Fstate%2Fpostgres"
	)

	tests := []struct {
		name    string
		run     bool
		pasted  string
		joinErr error
		removed bool
		after   string
	}{
		{
			name:    "the Postgres it made is removed when joining fails",
			run:     true,
			joinErr: errors.New("migrate refused"),
			removed: true,
		},
		{
			name:    "a Postgres the person runs is left alone when joining fails",
			pasted:  ownerURL,
			joinErr: errors.New("migrate refused"),
		},
		{
			name:  "komrad runs Postgres",
			run:   true,
			after: "[connection]\nrun_postgres = true\nowner_database_url = \"" + ownerURL + "\"\ndatabase_url = \"" + engineURL + "\"\n",
		},
		{
			name:   "a Postgres the person runs",
			pasted: " " + ownerURL + "\n",
			after:  "[connection]\nowner_database_url = \"" + ownerURL + "\"\ndatabase_url = \"" + engineURL + "\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := homeConfig(t)
			var joined string
			var removed bool
			step := &DatabaseStep{
				File:           &ConfigFile{Path: path},
				Stack:          config.Stack{},
				RunFlag:        tt.run,
				OwnerURLFlag:   tt.pasted,
				CreatePostgres: func(context.Context) (string, error) { return ownerURL, nil },
				RemovePostgres: func(context.Context) error {
					removed = true

					return nil
				},
				JoinEngine: func(_ context.Context, owner string) (string, error) {
					joined = owner

					return engineURL, tt.joinErr
				},
			}
			require.NoError(t, step.answerUnattended())

			err := step.repair(t.Context())

			if tt.joinErr != nil {
				require.ErrorIs(t, err, tt.joinErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, ownerURL, joined)
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
