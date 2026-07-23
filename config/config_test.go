package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaultsDBPathUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_AGENTS_DB", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".claude", "claude-agents.db"), cfg.DBPath)
	assert.Equal(t, "127.0.0.1:8787", cfg.Addr, "the addr default applies")
	assert.Equal(t, "default", cfg.TmuxSocket, "the socket default applies")
}

func TestLoadHonorsExplicitDBPath(t *testing.T) {
	t.Setenv("CLAUDE_AGENTS_DB", "/custom/agents.db")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "/custom/agents.db", cfg.DBPath)
}
