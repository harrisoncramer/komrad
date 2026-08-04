package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadAddsProcessEnvironment(t *testing.T) {
	t.Setenv("EDITOR", "nvim")
	t.Setenv("TMUX", "/tmp/tmux,1,0")
	t.Setenv("TMUX_PANE", "%4")
	t.Setenv("WEAVE_AGENT_ID", "agent-1")
	t.Setenv("WEAVE_SESSION_ID", "session-1")

	connection := Environment{Addr: "127.0.0.1:9999", CacheDir: "/tmp/weave-cache"}

	got := Load(connection)

	assert.Equal(t, connection.Addr, got.Addr)
	assert.Equal(t, connection.CacheDir, got.CacheDir)
	assert.Equal(t, "nvim", got.Editor)
	assert.Equal(t, "/tmp/tmux,1,0", got.Tmux)
	assert.Equal(t, "%4", got.TmuxPane)
	assert.Equal(t, "agent-1", got.AgentID)
	assert.Equal(t, "session-1", got.SessionID)
	assert.Equal(t, "/tmp/weave-cache/weave.log", got.LogPath())
}

func TestLoadDefaultsEditor(t *testing.T) {
	t.Setenv("EDITOR", "")

	assert.Equal(t, "vi", Load(Environment{}).Editor)
}

func TestLayerPathsUsesPlatformPathList(t *testing.T) {
	want := []string{"/one.toml", "/two.toml"}
	t.Setenv("WEAVE_CONFIG", strings.Join(want, string(filepath.ListSeparator)))

	assert.Equal(t, want, LayerPaths())
}
