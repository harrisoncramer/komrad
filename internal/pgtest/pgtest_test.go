package pgtest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"weave/config"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLeaked pins which databases a sweep is willing to drop, since worktrees on one machine
// share a server and dropping a database out from under a live run would break it.
func TestLeaked(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

	named := func(at time.Time) string {
		return fmt.Sprintf("%s%s_beefbeefbeefbeef", namePrefix, at.Format(nameTimeLayout))
	}

	tests := []struct {
		name  string
		names []string
		want  []string
	}{
		{
			name:  "a database older than the age a run can reach",
			names: []string{named(now.Add(-2 * time.Hour))},
			want:  []string{named(now.Add(-2 * time.Hour))},
		},
		{
			name:  "a database a run could still be holding",
			names: []string{named(now.Add(-time.Minute))},
		},
		{
			name:  "a database created this instant",
			names: []string{named(now)},
		},
		{
			name:  "a name left by an older build",
			names: []string{fmt.Sprintf("%s%d_1", namePrefix, 48100)},
		},
		{
			name:  "a name carrying no time at all",
			names: []string{namePrefix + "nonsense"},
		},
		{
			name:  "a name that is only the prefix",
			names: []string{namePrefix},
		},
		{
			name:  "a database belonging to something else",
			names: []string{"weave", "postgres"},
		},
		{
			name:  "the leaked ones picked out of a mixed server",
			names: []string{"weave", named(now), named(now.Add(-3 * time.Hour)), namePrefix + "48100_1"},
			want:  []string{named(now.Add(-3 * time.Hour))},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, leaked(tt.names, now))
		})
	}
}

// TestURLIgnoresLeftoverDatabases plants a database under every name an interrupted run of this
// binary could have picked, since a leftover used to make the next run's create fail outright.
func TestURLIgnoresLeftoverDatabases(t *testing.T) {
	cfg, err := config.Load(nil)
	require.NoError(t, err)

	adminURL := replaceDatabase(cfg.Connection.DatabaseURL, "postgres")

	admin, err := pgx.Connect(t.Context(), adminURL)
	if err != nil {
		t.Skipf("no postgres to test against; try 'mise run docker:up': %v", err)
	}
	defer func() { _ = admin.Close(t.Context()) }()

	var planted []string

	for counter := 1; counter <= 8; counter++ {
		name := fmt.Sprintf("%s%d_%d", namePrefix, os.Getpid(), counter)

		_, err := admin.Exec(t.Context(), fmt.Sprintf("CREATE DATABASE %q", name))
		require.NoError(t, err)

		t.Cleanup(func() {
			dropCtx := context.Background()

			conn, err := pgx.Connect(dropCtx, adminURL)
			if err != nil {
				t.Logf("failed to reach postgres to drop %q: %v", name, err)
				return
			}
			defer func() { _ = conn.Close(dropCtx) }()

			if err := drop(dropCtx, conn, name); err != nil {
				t.Logf("failed to drop planted database %q: %v", name, err)
			}
		})

		planted = append(planted, name)
	}

	got := URL(t)

	for _, name := range planted {
		assert.NotContains(t, got, name)
	}

	assert.Contains(t, got, "/"+namePrefix)
}
