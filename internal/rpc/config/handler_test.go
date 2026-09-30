package config

import (
	"errors"
	"testing"

	komradconfig "komrad/config"
	"komrad/internal/version"
	configv1 "komrad/proto/gen/config/v1"
	configv1komrad "komrad/proto/gen/config/v1/configv1komrad"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestGetConfig pins what the handler renders from each answer of its loader.
func TestGetConfig(t *testing.T) {
	loaded := komradconfig.Defaults()
	loaded.Harness.Claude.Flags = "--model opus"

	tests := []struct {
		name      string
		cfg       *komradconfig.Config
		err       error
		wantError string
		wantFlags any
	}{
		{
			name:      "a file that will not parse answers the defaults and the reason",
			err:       errors.New("failed to parse config"),
			wantError: "failed to parse config",
			wantFlags: "",
		},
		{
			name:      "a file that loads answers what it holds",
			cfg:       loaded,
			wantFlags: "--model opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := configv1komrad.ConfigServiceAdapter{Impl: NewHandler(func() (*komradconfig.Config, error) {
				return tt.cfg, tt.err
			})}

			resp, err := svc.GetConfig(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)

			assert.Equal(t, tt.wantError, resp.Msg.GetConfigError())
			assert.Equal(t, version.Version(), resp.Msg.GetVersion())
			assert.Equal(t, tt.wantFlags, lookup(t, resp.Msg, "harness", "claude", "flags"))
			assert.Equal(t, []any{"z"}, lookup(t, resp.Msg, "keybindings", "agents", "archive"))
		})
	}
}

// TestGetConfigRedactsTheConnectionStrings pins that no database password reaches the caller.
func TestGetConfigRedactsTheConnectionStrings(t *testing.T) {
	cfg := komradconfig.Defaults()
	cfg.Connection.DatabaseURL = "postgres://komrad:hunter2@db.internal:5432/panel"
	cfg.Connection.ReadOnlyDatabaseURL = "postgres://reader:alsosecret@db.internal:5432/panel"
	cfg.Connection.OwnerDatabaseURL = "postgres://owner:topsecret@db.internal:5432/panel"
	svc := configv1komrad.ConfigServiceAdapter{Impl: NewHandler(func() (*komradconfig.Config, error) {
		return cfg, nil
	})}

	resp, err := svc.GetConfig(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	rendered := resp.Msg.GetConfig().String()
	assert.NotContains(t, rendered, "hunter2")
	assert.NotContains(t, rendered, "alsosecret")
	assert.NotContains(t, rendered, "topsecret")
	assert.Equal(t, "postgres://komrad@db.internal:5432/panel", lookup(t, resp.Msg, "connection", "database_url"))
}

func lookup(t *testing.T, resp *configv1.GetConfigResponse, path ...string) any {
	t.Helper()

	var current any = resp.GetConfig().AsMap()
	for _, key := range path {
		object, ok := current.(map[string]any)
		require.True(t, ok, "the blob nests %v", path)

		current, ok = object[key]
		require.True(t, ok, "the blob carries %v", path)
	}

	return current
}
