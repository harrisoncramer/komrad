// Package config serves ConfigService over ConnectRPC.
package config

import (
	"context"
	"fmt"

	"claude-agents/internal/appconfig"
	configv1 "claude-agents/proto/gen/config/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// loader reads the user's config file. The handler holds the loader rather than a
// parsed config so the file is read per call. The server is spawned once and
// outlives many panel opens, so a config edited in between would otherwise stay
// invisible until somebody restarted it.
type loader func() (*appconfig.Config, error)

// Service implements configv1connect.ConfigServiceHandler.
type Service struct {
	load loader
}

// NewService constructs the handler from the loader it reads the config through.
func NewService(load loader) *Service {
	return &Service{load: load}
}

// GetConfig returns the configuration in force. A file that will not parse is
// answered with the built-in defaults and the reason it failed rather than an
// error, since the defaults are what the panel falls back to as well, and a caller
// told only that something is broken learns nothing about what is running.
func (s *Service) GetConfig(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[configv1.GetConfigResponse], error) {
	var configError *string

	cfg, err := s.load()
	if err != nil {
		cfg = appconfig.Defaults()
		configError = proto.String(err.Error())
	}

	blob, err := renderConfig(cfg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to render the config: %w", err))
	}

	return connect.NewResponse(&configv1.GetConfigResponse{Config: blob, ConfigError: configError}), nil
}
