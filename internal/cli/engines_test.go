package cli

import (
	"os"
	"path/filepath"
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenEngineArgs covers the open the picker hands off to, which has to carry the same
// config files this one was given.
func TestOpenEngineArgs(t *testing.T) {
	tests := []struct {
		name  string
		stack config.Stack
		want  []string
	}{
		{
			name:  "the default config",
			stack: config.Stack{},
			want:  []string{"/opt/weave", "open", "--engine", "remote"},
		},
		{
			name:  "named config files, in order",
			stack: config.Stack{Paths: []string{"/a.toml", "/b.toml"}},
			want:  []string{"/opt/weave", "open", "--engine", "remote", "--config", "/a.toml", "--config", "/b.toml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, openEngineArgs("/opt/weave", tt.stack, "remote"))
		})
	}
}

// TestTmuxRefusesAnEngine covers the one key there is, which no engine gets one of.
func TestTmuxRefusesAnEngine(t *testing.T) {
	state := &app{configStack: config.Stack{Engine: "remote"}}

	err := tmuxCmd(state).RunE(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "an engine has no panel key of its own")
}

// TestEngineRowsStartWithThisMachine covers what the picker is offered on a machine with and
// without engine files.
func TestEngineRowsStartWithThisMachine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"WEAVE_CONFIG", "WEAVE_ADDR", "WEAVE_TMUX_SOCKET", "WEAVE_CACHE_DIR", "WEAVE_DATABASE_URL", "WEAVE_READONLY_DATABASE_URL"} {
		t.Setenv(name, "")
	}

	rows, err := engineRows(config.Stack{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Empty(t, rows[0].Name)
	assert.Equal(t, "this machine", rows[0].Title())

	engines := filepath.Join(home, ".config", "weave", "engines")
	require.NoError(t, os.MkdirAll(engines, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(engines, "remote.toml"),
		[]byte("[connection]\naddr = \"127.0.0.1:8799\"\nssh = \"weave@host\"\ntmux_socket = \"weave-remote\"\n"), 0o600))

	rows, err = engineRows(config.Stack{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "remote", rows[1].Name)

	facts := map[string]string{}
	for _, fact := range rows[1].Facts {
		facts[fact.Label] = fact.Value
	}

	assert.Equal(t, filepath.Join(engines, "remote.toml"), facts["file   "])
	assert.Equal(t, "127.0.0.1:8799", facts["addr   "])
	assert.Equal(t, "weave-remote", facts["tmux   "])
	assert.Contains(t, facts["engine "], "weave@host")
}
