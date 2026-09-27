package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/store"
	"komrad/internal/ui"
	"komrad/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSwitchEngineArgs covers the open a panel hands its terminal to.
func TestSwitchEngineArgs(t *testing.T) {
	tests := []struct {
		name   string
		params switchEngineArgsParams
		want   []string
	}{
		{
			name:   "the default config",
			params: switchEngineArgsParams{self: "/opt/komrad", to: "remote"},
			want:   []string{"/opt/komrad", "open", "--engine", "remote"},
		},
		{
			name:   "named config files, in order",
			params: switchEngineArgsParams{self: "/opt/komrad", paths: []string{"/a.toml", "/b.toml"}, to: "remote"},
			want:   []string{"/opt/komrad", "open", "--engine", "remote", "--config", "/a.toml", "--config", "/b.toml"},
		},
		{
			name: "the host client and the engine to fall back to",
			params: switchEngineArgsParams{
				self:     "/opt/komrad",
				to:       "local",
				host:     hostPlace{socket: "/tmp/tmux-501/default", path: "/src"},
				fallback: "remote",
			},
			want: []string{
				"/opt/komrad", "open", "--engine", "local",
				"--host-socket", "/tmp/tmux-501/default", "--host-path", "/src", "--fallback-engine", "remote",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, switchEngineArgs(tt.params))
		})
	}
}

// TestEngineRowsStartWithThisMachine covers what the chooser is offered on a machine with and
// without engine files.
func TestEngineRowsStartWithThisMachine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"KOMRAD_CONFIG", "KOMRAD_ADDR", "KOMRAD_TMUX_SOCKET", "KOMRAD_CACHE_DIR", "KOMRAD_DATABASE_URL", "KOMRAD_READONLY_DATABASE_URL"} {
		t.Setenv(name, "")
	}

	rows, err := engineRows(config.Stack{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Empty(t, rows[0].Name)
	assert.Equal(t, "local", rows[0].Title())

	engines := filepath.Join(home, ".config", "komrad", "engines")
	require.NoError(t, os.MkdirAll(engines, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(engines, "remote.toml"),
		[]byte("[connection]\naddr = \"127.0.0.1:8799\"\nssh = \"komrad@host\"\ntmux_socket = \"komrad-remote\"\n"), 0o600))

	rows, err = engineRows(config.Stack{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "remote", rows[1].Name)

	facts := map[string]string{}
	for _, fact := range rows[1].Facts {
		facts[fact.Label] = fact.Value
	}

	assert.Equal(t, filepath.Join(engines, "remote.toml"), facts["file"])
	assert.Equal(t, "127.0.0.1:8799", facts["addr"])
	assert.Equal(t, "komrad-remote", facts["tmux"])
	assert.Contains(t, facts["engine"], "komrad@host")
}

// TestProbeWarnsOfAFailingLoginAndRefusesOnlyWhatCannotStart pins a bad password showing in the chooser without locking out a running engine.
func TestProbeWarnsOfAFailingLoginAndRefusesOnlyWhatCannotStart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"KOMRAD_CONFIG", "KOMRAD_ADDR", "KOMRAD_TMUX_SOCKET", "KOMRAD_CACHE_DIR", "KOMRAD_DATABASE_URL", "KOMRAD_READONLY_DATABASE_URL", "KOMRAD_OWNER_DATABASE_URL"} {
		t.Setenv(name, "")
	}

	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"status":"ok","version":%q,"commit":%q}`, version.Version(), version.Commit())
	}))
	t.Cleanup(answering.Close)

	engines := filepath.Join(home, ".config", "komrad", "engines")
	require.NoError(t, os.MkdirAll(engines, 0o755))
	write := func(name, addr string) {
		require.NoError(t, os.WriteFile(filepath.Join(engines, name+".toml"), []byte(
			"[connection]\ndatabase_url = \"postgres://komrad_engine_second:wrong@localhost:5460/komrad\"\naddr = \""+addr+"\"\ntmux_socket = \"komrad-"+name+"\"\n"), 0o600))
	}
	write("stopped", "127.0.0.1:1")
	write("running", strings.TrimPrefix(answering.URL, "http://"))

	failing := func(err error) engineProber {
		return engineProber{checkLogin: func(context.Context, string) error { return err }}
	}
	badPassword := errors.New("password authentication failed")

	stopped := failing(badPassword).probe(t.Context(), ui.EngineRow{Name: "stopped"})
	assert.Equal(t, "login", stopped.Label)
	assert.Contains(t, stopped.Refused, "password authentication failed")

	noEngine := failing(store.ErrNotAnEngine).probe(t.Context(), ui.EngineRow{Name: "stopped"})
	assert.Equal(t, "no engine", noEngine.Label)

	running := failing(badPassword).probe(t.Context(), ui.EngineRow{Name: "running"})
	assert.Empty(t, running.Refused, "a running server can still be attached to")
	assert.Contains(t, running.Build, "could not start again")

	healthy := failing(nil).probe(t.Context(), ui.EngineRow{Name: "stopped"})
	assert.Empty(t, healthy.Refused)
}
