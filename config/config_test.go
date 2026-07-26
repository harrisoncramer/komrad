package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaultsToTheDevelopmentDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, defaultDatabaseURL, cfg.DatabaseURL)
	assert.Equal(t, "127.0.0.1:8787", cfg.Addr, "the addr default applies")
	assert.Equal(t, "default", cfg.TmuxSocket, "the socket default applies")
}

func TestLoadHonorsExplicitDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://someone@db.example.com:5432/agents")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "postgres://someone@db.example.com:5432/agents", cfg.DatabaseURL)
}
