package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	weaveconfig "weave/config"
	"weave/internal/version"
	configv1 "weave/proto/gen/config/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestGetConfig covers each state the user's file can be in, and a file that will not parse
// answering with the defaults and the reason rather than failing the call.
func TestGetConfig(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		check func(t *testing.T, resp *configv1.GetConfigResponse)
	}{
		{
			name: "a key the config knows nothing about, which is a typo in the file",
			file: `bogus = true`,
			check: func(t *testing.T, resp *configv1.GetConfigResponse) {
				assert.Contains(t, resp.GetConfigError(), "failed to parse config")
				assert.Equal(t, []any{"t"}, binding(t, resp, "marks", "set"))
			},
		},
		{
			name: "no config file at all, which is every install that never wrote one",
			check: func(t *testing.T, resp *configv1.GetConfigResponse) {
				assert.Nil(t, resp.ConfigError)
				assert.Empty(t, harnessFlags(t, resp, "claude"))
				assert.Equal(t, []any{"k"}, binding(t, resp, "nav", "up"))
			},
		},
		{
			name: "flags the user wants on every agent of one CLI",
			file: "[harness.claude]\nflags = \"--model opus\"\n",
			check: func(t *testing.T, resp *configv1.GetConfigResponse) {
				assert.Nil(t, resp.ConfigError)
				assert.Equal(t, "--model opus", harnessFlags(t, resp, "claude"))
			},
		},
		{
			name: "one rebound key, which leaves every other binding on its default",
			file: "[keybindings.marks]\nset = [\"T\"]\n",
			check: func(t *testing.T, resp *configv1.GetConfigResponse) {
				assert.Equal(t, []any{"T"}, binding(t, resp, "marks", "set"))
				assert.Equal(t, []any{"z"}, binding(t, resp, "agents", "archive"))
			},
		},
		{
			name: "a screen the user's file never mentions",
			file: `default_harness = "claude"`,
			check: func(t *testing.T, resp *configv1.GetConfigResponse) {
				assert.Equal(t, []any{"Z"}, binding(t, resp, "archive", "open"))
				assert.Equal(t, []any{"r"}, binding(t, resp, "archive", "restore"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			if tt.file != "" {
				path := filepath.Join(t.TempDir(), "config.toml")
				require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o600))
				paths = []string{path}
			} else {
				t.Setenv("HOME", t.TempDir())
			}

			svc := NewService(func() (*weaveconfig.Config, error) { return weaveconfig.Load(paths) })

			resp, err := svc.GetConfig(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)
			tt.check(t, resp.Msg)
		})
	}
}

// TestGetConfigReportsTheBuild covers the build in the response, which is the only way an
// agent reaching this server over MCP can say which one it was talking to.
func TestGetConfigReportsTheBuild(t *testing.T) {
	svc := NewService(func() (*weaveconfig.Config, error) { return weaveconfig.Defaults(), nil })

	resp, err := svc.GetConfig(t.Context(), connect.NewRequest(&emptypb.Empty{}))

	require.NoError(t, err)
	assert.Equal(t, version.Version(), resp.Msg.GetVersion())
}

// TestGetConfigKeysMatchTheFile pins the names the blob is keyed by, which a reader has to find
// the same setting in their own TOML file under.
func TestGetConfigKeysMatchTheFile(t *testing.T) {
	tests := []struct {
		name string
		path []string
	}{
		{
			name: "which CLI a new agent runs",
			path: []string{"default_harness"},
		},
		{
			name: "the flags every agent of one CLI inherits",
			path: []string{"harness", "claude", "flags"},
		},
		{
			name: "navigation",
			path: []string{"keybindings", "nav"},
		},
		{
			name: "the agents list",
			path: []string{"keybindings", "agents"},
		},
		{
			name: "the forms",
			path: []string{"keybindings", "form"},
		},
		{
			name: "workflows",
			path: []string{"keybindings", "workflows"},
		},
		{
			name: "schedules",
			path: []string{"keybindings", "schedules"},
		},
		{
			name: "the archive",
			path: []string{"keybindings", "archive"},
		},
	}

	svc := NewService(func() (*weaveconfig.Config, error) {
		return &weaveconfig.Config{Keybindings: weaveconfig.DefaultKeybindings()}, nil
	})

	resp, err := svc.GetConfig(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := lookup(resp.Msg.GetConfig().AsMap(), tt.path)
			assert.True(t, ok, "the blob carries %s", strings.Join(tt.path, "."))
		})
	}
}

func binding(t *testing.T, resp *configv1.GetConfigResponse, screen, name string) []any {
	t.Helper()

	found, ok := lookup(resp.GetConfig().AsMap(), []string{"keybindings", screen, name})
	require.True(t, ok, "the %s screen binds %s", screen, name)

	keys, isList := found.([]any)
	require.True(t, isList, "%s is bound to a list of keys", name)

	return keys
}

func lookup(fields map[string]any, path []string) (any, bool) {
	var current any = fields

	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}

		current, ok = object[key]
		if !ok {
			return nil, false
		}
	}

	return current, true
}

func harnessFlags(t *testing.T, resp *configv1.GetConfigResponse, id string) any {
	t.Helper()

	flags, ok := lookup(resp.GetConfig().AsMap(), []string{"harness", id, "flags"})
	require.True(t, ok, "the blob carries harness.%s.flags", id)

	return flags
}

// TestGetConfigRedactsTheConnectionStrings covers the database password in the file, which an
// unredacted answer would hand to any model reaching this endpoint over MCP.
func TestGetConfigRedactsTheConnectionStrings(t *testing.T) {
	for _, name := range []string{"WEAVE_DATABASE_URL", "WEAVE_READONLY_DATABASE_URL"} {
		t.Setenv(name, "")
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[connection]\n" +
		"database_url = \"postgres://weave:hunter2@db.internal:5432/panel\"\n" +
		"readonly_database_url = \"postgres://reader:alsosecret@db.internal:5432/panel\"\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	svc := NewService(func() (*weaveconfig.Config, error) { return weaveconfig.Load([]string{path}) })

	resp, err := svc.GetConfig(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	rendered := resp.Msg.GetConfig().String()
	assert.NotContains(t, rendered, "hunter2")
	assert.NotContains(t, rendered, "alsosecret")
	assert.Contains(t, rendered, "postgres://weave@db.internal:5432/panel",
		"the login and the address are kept, since those are what a bug report needs")
}
