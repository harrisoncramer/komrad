package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"weave/internal/appconfig"
	configv1 "weave/proto/gen/config/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestGetConfig covers what a caller is told for each state the user's file can be
// in. A file that will not parse is the one worth watching. It answers with the
// defaults the panel falls back to and the reason, rather than failing the call.
func TestGetConfig(t *testing.T) {
	tests := []struct {
		name string
		// file is written to the config path before the call. An empty one writes
		// nothing, so the loader resolves a path that is not there.
		file  string
		check func(t *testing.T, resp *configv1.GetConfigResponse)
	}{
		{
			name: "a key the config knows nothing about, which is a typo or a binding left by an older version",
			file: `bogus = true`,
			check: func(t *testing.T, resp *configv1.GetConfigResponse) {
				assert.Contains(t, resp.GetConfigError(), "failed to parse config")
				assert.Equal(t, []any{"m"}, binding(t, resp, "agents", "mark"))
			},
		},
		{
			name: "no config file at all, which is every install that never wrote one",
			check: func(t *testing.T, resp *configv1.GetConfigResponse) {
				assert.Nil(t, resp.ConfigError)
				assert.Empty(t, harnessFlags(t, resp, "claude"))
				assert.Equal(t, []any{"up", "ctrl+p"}, binding(t, resp, "nav", "up"))
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
			file: "[keybindings.agents]\nmark = [\"M\"]\n",
			check: func(t *testing.T, resp *configv1.GetConfigResponse) {
				assert.Equal(t, []any{"M"}, binding(t, resp, "agents", "mark"))
				assert.Equal(t, []any{"d"}, binding(t, resp, "agents", "delete"))
			},
		},
		{
			name: "a screen the user's file never mentions",
			file: `default_harness = "claude"`,
			check: func(t *testing.T, resp *configv1.GetConfigResponse) {
				assert.Equal(t, []any{"t"}, binding(t, resp, "trash", "open"))
				assert.Equal(t, []any{"r"}, binding(t, resp, "trash", "restore"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if tt.file != "" {
				require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o600))
			}

			svc := NewService(func() (*appconfig.Config, error) { return appconfig.Load(path) })

			resp, err := svc.GetConfig(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)
			tt.check(t, resp.Msg)
		})
	}
}

// TestGetConfigKeysMatchTheFile pins the names the blob is keyed by, and that the
// screens stay nested under keybindings rather than flattened. The names are the
// only thing a caller has to go on, and a reader who sees one has to be able to
// find the same setting in their own TOML file.
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
			name: "the trash",
			path: []string{"keybindings", "trash"},
		},
	}

	svc := NewService(func() (*appconfig.Config, error) {
		return &appconfig.Config{Keybindings: appconfig.DefaultKeybindings()}, nil
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

// binding digs one binding's keys out of the blob, so a row can name the screen and
// the binding rather than walking to them itself.
func binding(t *testing.T, resp *configv1.GetConfigResponse, screen, name string) []any {
	t.Helper()

	found, ok := lookup(resp.GetConfig().AsMap(), []string{"keybindings", screen, name})
	require.True(t, ok, "the %s screen binds %s", screen, name)

	keys, isList := found.([]any)
	require.True(t, isList, "%s is bound to a list of keys", name)

	return keys
}

// lookup walks the blob to the value at a path, reporting whether every key along
// the way was there.
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

// harnessFlags digs one harness's configured flags out of the blob, so a row names
// the harness rather than walking to it.
func harnessFlags(t *testing.T, resp *configv1.GetConfigResponse, id string) any {
	t.Helper()

	flags, ok := lookup(resp.GetConfig().AsMap(), []string{"harness", id, "flags"})
	require.True(t, ok, "the blob carries harness.%s.flags", id)

	return flags
}
