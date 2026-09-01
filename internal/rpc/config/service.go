// Package config serves the configuration RPC, which reports the settings weave is running on
// as they were resolved rather than as they were written.
package config

import (
	"context"

	weaveconfig "weave/config"
	"weave/internal/rpcerr"
	"weave/internal/version"
	configv1 "weave/proto/gen/config/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type loader func() (*weaveconfig.Config, error)

type Service struct {
	load loader
}

// NewService constructs the handler from the loader it reads the config through.
func NewService(load loader) *Service {
	return &Service{load: load}
}

// GetConfig returns the configuration in force and the build answering, falling back to the
// built-in defaults and the reason when the user's file will not parse.
func (s *Service) GetConfig(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[configv1.GetConfigResponse], error) {
	var configError *string

	cfg, err := s.load()
	if err != nil {
		cfg = weaveconfig.Defaults()
		configError = proto.String(err.Error())
	}

	blob, err := renderConfig(cfg)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to render the config")
	}

	return connect.NewResponse(&configv1.GetConfigResponse{
		Config:      blob,
		ConfigError: configError,
		Version:     version.Version(),
	}), nil
}
